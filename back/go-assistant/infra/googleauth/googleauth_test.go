package googleauth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/googleauth"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

const testClientID = "test-client-id.apps.googleusercontent.com"

// ── Test Helpers ───────────────────────────────────────────────────────────

// testKeyPair holds an RSA key pair and its kid for testing.
type testKeyPair struct {
	kid        string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

// generateKeyPair creates a fresh RSA-2048 key pair for testing.
func generateKeyPair(t *testing.T, kid string) *testKeyPair {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return &testKeyPair{
		kid:        kid,
		privateKey: privKey,
		publicKey:  &privKey.PublicKey,
	}
}

// base64URLEncode encodes bytes as base64url without padding.
func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// buildJWKS creates a JWKS JSON response from the given key pairs.
func buildJWKS(keys ...*testKeyPair) []byte {
	type jwkKey struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		Alg string `json:"alg"`
		Use string `json:"use"`
		N   string `json:"n"`
		E   string `json:"e"`
	}

	var jwksKeys []jwkKey
	for _, k := range keys {
		eBytes := big.NewInt(int64(k.publicKey.E)).Bytes()
		jwksKeys = append(jwksKeys, jwkKey{
			Kid: k.kid,
			Kty: "RSA",
			Alg: "RS256",
			Use: "sig",
			N:   base64URLEncode(k.publicKey.N.Bytes()),
			E:   base64URLEncode(eBytes),
		})
	}

	data, _ := json.Marshal(map[string]any{"keys": jwksKeys})
	return data
}

// signJWT creates a signed JWT with the given header and payload using RS256.
func signJWT(t *testing.T, kp *testKeyPair, headerJSON, payloadJSON []byte) string {
	t.Helper()

	headerB64 := base64URLEncode(headerJSON)
	payloadB64 := base64URLEncode(payloadJSON)
	signingInput := headerB64 + "." + payloadB64

	// SHA-256 hash and sign.
	hash := crypto.SHA256.New()
	hash.Write([]byte(signingInput))
	hashed := hash.Sum(nil)

	sig, err := rsa.SignPKCS1v15(rand.Reader, kp.privateKey, crypto.SHA256, hashed)
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}

	return signingInput + "." + base64URLEncode(sig)
}

// buildHeader creates a JWT header JSON.
func buildHeader(kid, alg string) []byte {
	data, _ := json.Marshal(map[string]string{
		"alg": alg,
		"kid": kid,
		"typ": "JWT",
	})
	return data
}

// buildPayload creates a JWT payload JSON with the given claims.
func buildPayload(sub, email, name, picture, iss, aud string, exp int64) []byte {
	data, _ := json.Marshal(map[string]any{
		"sub":     sub,
		"email":   email,
		"name":    name,
		"picture": picture,
		"iss":     iss,
		"aud":     aud,
		"exp":     exp,
		"iat":     time.Now().Unix(),
	})
	return data
}

// startJWKSServer starts a test HTTP server serving JWKS.
func startJWKSServer(t *testing.T, jwksData []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksData)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestGoogleTokenVerifier_ValidToken(t *testing.T) {
	t.Parallel()

	kp := generateKeyPair(t, "key-1")
	jwksSrv := startJWKSServer(t, buildJWKS(kp))

	verifier := googleauth.NewGoogleTokenVerifierForTest(testClientID, jwksSrv.URL, time.Now)

	header := buildHeader("key-1", "RS256")
	payload := buildPayload(
		"google-sub-123",
		"user@example.com",
		"Test User",
		"https://example.com/pic.jpg",
		"accounts.google.com",
		testClientID,
		time.Now().Add(1*time.Hour).Unix(),
	)
	token := signJWT(t, kp, header, payload)

	user, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify() error = %v; want nil", err)
	}
	if user.Sub != "google-sub-123" {
		t.Errorf("Sub = %q; want %q", user.Sub, "google-sub-123")
	}
	if user.Email != "user@example.com" {
		t.Errorf("Email = %q; want %q", user.Email, "user@example.com")
	}
	if user.Name != "Test User" {
		t.Errorf("Name = %q; want %q", user.Name, "Test User")
	}
	if user.Picture != "https://example.com/pic.jpg" {
		t.Errorf("Picture = %q; want %q", user.Picture, "https://example.com/pic.jpg")
	}
}

func TestGoogleTokenVerifier_AlternateIssuer(t *testing.T) {
	t.Parallel()

	kp := generateKeyPair(t, "key-1")
	jwksSrv := startJWKSServer(t, buildJWKS(kp))

	verifier := googleauth.NewGoogleTokenVerifierForTest(testClientID, jwksSrv.URL, time.Now)

	header := buildHeader("key-1", "RS256")
	payload := buildPayload("sub-1", "a@b.com", "A", "", "https://accounts.google.com", testClientID, time.Now().Add(1*time.Hour).Unix())
	token := signJWT(t, kp, header, payload)

	user, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify() error = %v; want nil", err)
	}
	if user.Sub != "sub-1" {
		t.Errorf("Sub = %q; want %q", user.Sub, "sub-1")
	}
}

func TestGoogleTokenVerifier_Errors(t *testing.T) {
	t.Parallel()

	kp := generateKeyPair(t, "key-1")
	kp2 := generateKeyPair(t, "key-2") // different key for bad signature test
	jwksSrv := startJWKSServer(t, buildJWKS(kp))

	now := time.Now()
	nowFunc := func() time.Time { return now }

	verifier := googleauth.NewGoogleTokenVerifierForTest(testClientID, jwksSrv.URL, nowFunc)

	tests := []struct {
		name      string
		token     string
		wantErr   error
		wantInMsg string
	}{
		{
			name:    "empty token",
			token:   "",
			wantErr: auth.ErrInvalidToken,
		},
		{
			name:    "malformed - no dots",
			token:   "notajwt",
			wantErr: auth.ErrInvalidToken,
		},
		{
			name:    "malformed - one dot",
			token:   "header.payload",
			wantErr: auth.ErrInvalidToken,
		},
		{
			name: "expired token",
			token: signJWT(t, kp,
				buildHeader("key-1", "RS256"),
				buildPayload("sub", "e@e.com", "N", "", "accounts.google.com", testClientID, now.Add(-1*time.Hour).Unix()),
			),
			wantErr: auth.ErrTokenExpired,
		},
		{
			name: "wrong audience",
			token: signJWT(t, kp,
				buildHeader("key-1", "RS256"),
				buildPayload("sub", "e@e.com", "N", "", "accounts.google.com", "wrong-client-id", now.Add(1*time.Hour).Unix()),
			),
			wantErr: auth.ErrWrongAudience,
		},
		{
			name: "wrong issuer",
			token: signJWT(t, kp,
				buildHeader("key-1", "RS256"),
				buildPayload("sub", "e@e.com", "N", "", "evil.example.com", testClientID, now.Add(1*time.Hour).Unix()),
			),
			wantErr:   auth.ErrInvalidToken,
			wantInMsg: "unexpected issuer",
		},
		{
			name: "missing sub claim",
			token: signJWT(t, kp,
				buildHeader("key-1", "RS256"),
				buildPayload("", "e@e.com", "N", "", "accounts.google.com", testClientID, now.Add(1*time.Hour).Unix()),
			),
			wantErr:   auth.ErrInvalidToken,
			wantInMsg: "missing sub",
		},
		{
			name: "unsupported algorithm",
			token: signJWT(t, kp,
				buildHeader("key-1", "HS256"),
				buildPayload("sub", "e@e.com", "N", "", "accounts.google.com", testClientID, now.Add(1*time.Hour).Unix()),
			),
			wantErr:   auth.ErrInvalidToken,
			wantInMsg: "unsupported algorithm",
		},
		{
			name: "bad signature - wrong key",
			token: signJWT(t, kp2, // signed with kp2, but JWKS only has kp
				buildHeader("key-1", "RS256"),
				buildPayload("sub", "e@e.com", "N", "", "accounts.google.com", testClientID, now.Add(1*time.Hour).Unix()),
			),
			wantErr:   auth.ErrInvalidToken,
			wantInMsg: "bad signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := verifier.Verify(context.Background(), tt.token)
			if err == nil {
				t.Fatal("Verify() error = nil; want error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Verify() error = %v; want %v", err, tt.wantErr)
			}
			if tt.wantInMsg != "" {
				if msg := err.Error(); !contains(msg, tt.wantInMsg) {
					t.Errorf("error message %q does not contain %q", msg, tt.wantInMsg)
				}
			}
		})
	}
}

func TestGoogleTokenVerifier_UnknownKidTriggersRefresh(t *testing.T) {
	t.Parallel()

	kp1 := generateKeyPair(t, "key-1")
	kp2 := generateKeyPair(t, "key-2")

	// Start with only key-1 in JWKS.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			// First call: only key-1.
			w.Write(buildJWKS(kp1))
		} else {
			// Subsequent calls: both keys (simulating key rotation).
			w.Write(buildJWKS(kp1, kp2))
		}
	}))
	t.Cleanup(srv.Close)

	verifier := googleauth.NewGoogleTokenVerifierForTest(testClientID, srv.URL, time.Now)

	// First: verify with key-1 (works, triggers initial JWKS fetch).
	token1 := signJWT(t, kp1,
		buildHeader("key-1", "RS256"),
		buildPayload("sub-1", "a@b.com", "A", "", "accounts.google.com", testClientID, time.Now().Add(1*time.Hour).Unix()),
	)
	if _, err := verifier.Verify(context.Background(), token1); err != nil {
		t.Fatalf("first Verify() error = %v", err)
	}

	// Second: verify with key-2 (unknown kid → triggers refresh → succeeds).
	token2 := signJWT(t, kp2,
		buildHeader("key-2", "RS256"),
		buildPayload("sub-2", "c@d.com", "C", "", "accounts.google.com", testClientID, time.Now().Add(1*time.Hour).Unix()),
	)
	user, err := verifier.Verify(context.Background(), token2)
	if err != nil {
		t.Fatalf("second Verify() error = %v; want nil (should refresh JWKS)", err)
	}
	if user.Sub != "sub-2" {
		t.Errorf("Sub = %q; want %q", user.Sub, "sub-2")
	}

	// Verify that JWKS was fetched at least twice (initial + refresh).
	if callCount < 2 {
		t.Errorf("JWKS fetch count = %d; want >= 2", callCount)
	}
}

func TestGoogleTokenVerifier_CacheExpiry(t *testing.T) {
	t.Parallel()

	kp := generateKeyPair(t, "key-1")

	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write(buildJWKS(kp))
	}))
	t.Cleanup(srv.Close)

	// Use a nowFunc that advances past the cache TTL.
	currentTime := time.Now()
	nowFunc := func() time.Time { return currentTime }

	verifier := googleauth.NewGoogleTokenVerifierForTest(testClientID, srv.URL, nowFunc)

	makeToken := func() string {
		return signJWT(t, kp,
			buildHeader("key-1", "RS256"),
			buildPayload("sub", "e@e.com", "N", "", "accounts.google.com", testClientID, currentTime.Add(48*time.Hour).Unix()),
		)
	}

	// First verify: triggers JWKS fetch.
	if _, err := verifier.Verify(context.Background(), makeToken()); err != nil {
		t.Fatalf("first Verify() error = %v", err)
	}
	if fetchCount != 1 {
		t.Fatalf("fetch count after first verify = %d; want 1", fetchCount)
	}

	// Second verify (same time): uses cache.
	if _, err := verifier.Verify(context.Background(), makeToken()); err != nil {
		t.Fatalf("second Verify() error = %v", err)
	}
	if fetchCount != 1 {
		t.Fatalf("fetch count after second verify = %d; want 1 (should use cache)", fetchCount)
	}

	// Advance time past TTL (25 hours).
	currentTime = currentTime.Add(25 * time.Hour)

	// Third verify: cache expired, triggers refresh.
	if _, err := verifier.Verify(context.Background(), makeToken()); err != nil {
		t.Fatalf("third Verify() error = %v", err)
	}
	if fetchCount != 2 {
		t.Errorf("fetch count after cache expiry = %d; want 2", fetchCount)
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
