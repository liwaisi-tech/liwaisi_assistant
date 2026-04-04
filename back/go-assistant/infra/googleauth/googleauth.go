// Package googleauth implements the auth.TokenVerifier port using Google's
// JWKS endpoint for RS256 signature verification of Google id_tokens.
// It caches public keys locally with a configurable TTL and supports
// automatic key rotation refresh.
package googleauth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// defaultJWKSURL is Google's public key endpoint for verifying id_token signatures.
const defaultJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

// defaultCacheTTL is how long JWKS keys are cached before refresh.
const defaultCacheTTL = 24 * time.Hour

// validIssuers are the accepted iss claim values for Google id_tokens.
var validIssuers = map[string]bool{
	"accounts.google.com":         true,
	"https://accounts.google.com": true,
}

// Compile-time interface check.
var _ auth.TokenVerifier = (*GoogleTokenVerifier)(nil)

// ── JWKS Types ─────────────────────────────────────────────────────────────

// jwksResponse is the JSON structure returned by Google's JWKS endpoint.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey is a single JSON Web Key in the JWKS response.
type jwkKey struct {
	Kid string `json:"kid"` // Key ID
	Kty string `json:"kty"` // Key type (RSA)
	Alg string `json:"alg"` // Algorithm (RS256)
	Use string `json:"use"` // Usage (sig)
	N   string `json:"n"`   // RSA modulus (base64url)
	E   string `json:"e"`   // RSA exponent (base64url)
}

// jwksCache stores parsed RSA public keys keyed by kid.
type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	ttl       time.Duration
}

func (c *jwksCache) get(kid string) (*rsa.PublicKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key, ok := c.keys[kid]
	return key, ok
}

func (c *jwksCache) isExpired(now time.Time) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return now.Sub(c.fetchedAt) > c.ttl
}

func (c *jwksCache) update(keys map[string]*rsa.PublicKey, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = keys
	c.fetchedAt = now
}

// ── Google Token Claims ────────────────────────────────────────────────────

// googleClaims represents the relevant claims in a Google id_token JWT.
type googleClaims struct {
	Iss     string `json:"iss"`
	Aud     string `json:"aud"`
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	Exp     int64  `json:"exp"`
	Iat     int64  `json:"iat"`
}

// ── JWT Header ─────────────────────────────────────────────────────────────

// jwtHeader represents the JOSE header of a JWT.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// ── GoogleTokenVerifier ────────────────────────────────────────────────────

// GoogleTokenVerifier verifies Google id_tokens using JWKS-based RS256
// signature validation. It caches public keys locally and refreshes
// them when an unknown kid is encountered or the cache TTL expires.
type GoogleTokenVerifier struct {
	clientID   string
	jwksURL    string
	httpClient *http.Client
	cache      *jwksCache
	nowFunc    func() time.Time // injectable for testing
}

// NewGoogleTokenVerifier creates a new verifier for the given Google OAuth client ID.
func NewGoogleTokenVerifier(clientID string) *GoogleTokenVerifier {
	return &GoogleTokenVerifier{
		clientID: clientID,
		jwksURL:  defaultJWKSURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: &jwksCache{
			keys: make(map[string]*rsa.PublicKey),
			ttl:  defaultCacheTTL,
		},
		nowFunc: time.Now,
	}
}

// NewGoogleTokenVerifierForTest creates a verifier with injectable JWKS URL
// and time function for testing. Not intended for production use.
func NewGoogleTokenVerifierForTest(clientID, jwksURL string, nowFunc func() time.Time) *GoogleTokenVerifier {
	v := NewGoogleTokenVerifier(clientID)
	v.jwksURL = jwksURL
	v.nowFunc = nowFunc
	return v
}

// Verify validates the Google id_token and returns the authenticated user.
// It performs: JWT structure validation, RS256 signature verification via JWKS,
// and claim validation (iss, aud, exp).
func (v *GoogleTokenVerifier) Verify(ctx context.Context, tokenString string) (*auth.AuthenticatedUser, error) {
	// Parse JWT structure (header.payload.signature).
	header, payload, signature, err := parseJWT(tokenString)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}

	// Decode header to get kid and alg.
	var hdr jwtHeader
	if err := json.Unmarshal(header, &hdr); err != nil {
		return nil, fmt.Errorf("%w: malformed header", auth.ErrInvalidToken)
	}
	if hdr.Alg != "RS256" {
		return nil, fmt.Errorf("%w: unsupported algorithm %q", auth.ErrInvalidToken, hdr.Alg)
	}

	// Get the RSA public key for this kid.
	pubKey, err := v.getKey(ctx, hdr.Kid)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}

	// Verify RS256 signature.
	signedContent := tokenString[:strings.LastIndex(tokenString, ".")]
	if err := verifyRS256(pubKey, []byte(signedContent), signature); err != nil {
		return nil, fmt.Errorf("%w: bad signature", auth.ErrInvalidToken)
	}

	// Decode and validate claims.
	var claims googleClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: malformed payload", auth.ErrInvalidToken)
	}

	if err := v.validateClaims(&claims); err != nil {
		return nil, err
	}

	return &auth.AuthenticatedUser{
		Sub:     claims.Sub,
		Email:   claims.Email,
		Name:    claims.Name,
		Picture: claims.Picture,
	}, nil
}

// ── Internal Methods ───────────────────────────────────────────────────────

// getKey retrieves the RSA public key for the given kid from the cache.
// If the kid is not found or the cache is expired, it refreshes from Google.
func (v *GoogleTokenVerifier) getKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	// Try cache first.
	if key, ok := v.cache.get(kid); ok && !v.cache.isExpired(v.nowFunc()) {
		return key, nil
	}

	// Refresh cache.
	if err := v.refreshKeys(ctx); err != nil {
		return nil, fmt.Errorf("jwks fetch failed: %w", err)
	}

	// Try again after refresh.
	key, ok := v.cache.get(kid)
	if !ok {
		return nil, fmt.Errorf("unknown key ID %q", kid)
	}
	return key, nil
}

// refreshKeys fetches the JWKS from Google and updates the local cache.
func (v *GoogleTokenVerifier) refreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks endpoint returned %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Use != "sig" {
			continue
		}
		pubKey, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue // skip malformed keys
		}
		keys[k.Kid] = pubKey
	}

	v.cache.update(keys, v.nowFunc())
	return nil
}

// validateClaims checks iss, aud, and exp claims.
func (v *GoogleTokenVerifier) validateClaims(claims *googleClaims) error {
	if !validIssuers[claims.Iss] {
		return fmt.Errorf("%w: unexpected issuer %q", auth.ErrInvalidToken, claims.Iss)
	}
	if claims.Aud != v.clientID {
		return fmt.Errorf("%w: audience %q does not match client ID", auth.ErrWrongAudience, claims.Aud)
	}
	if v.nowFunc().Unix() > claims.Exp {
		return auth.ErrTokenExpired
	}
	if claims.Sub == "" {
		return fmt.Errorf("%w: missing sub claim", auth.ErrInvalidToken)
	}
	return nil
}

// ── Pure Functions ─────────────────────────────────────────────────────────

// parseJWT splits a JWT into its three base64url-decoded parts.
func parseJWT(tokenString string) (header, payload, signature []byte, err error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, nil, nil, errors.New("token must have 3 parts")
	}

	header, err = base64URLDecode(parts[0])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("decode header: %w", err)
	}

	payload, err = base64URLDecode(parts[1])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("decode payload: %w", err)
	}

	signature, err = base64URLDecode(parts[2])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("decode signature: %w", err)
	}

	return header, payload, signature, nil
}

// base64URLDecode decodes a base64url-encoded string (no padding).
func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// verifyRS256 verifies an RS256 signature using the given RSA public key.
func verifyRS256(pubKey *rsa.PublicKey, message, signature []byte) error {
	hash := sha256.Sum256(message)
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature)
}

// parseRSAPublicKey constructs an RSA public key from base64url-encoded N and E.
func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64URLDecode(nStr)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}

	eBytes, err := base64URLDecode(eStr)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() {
		return nil, errors.New("exponent too large")
	}

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}
