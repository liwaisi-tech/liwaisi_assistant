package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// AuthMiddleware returns middleware that validates Bearer tokens on every request.
// If verifier is nil (dev-mode), all requests pass through with a synthetic dev-user.
// Token extraction order: Authorization header, then ?token query parameter (for SSE).
func AuthMiddleware(verifier auth.TokenVerifier, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Public routes: skip auth for health and version endpoints.
			if isPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Dev-mode: no verifier configured, inject synthetic user.
			if verifier == nil {
				ctx := auth.NewContext(r.Context(), &auth.AuthenticatedUser{
					Sub:   "dev-user",
					Email: "dev@localhost",
					Name:  "Developer",
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Extract token from Authorization header or query parameter.
			token := extractBearerToken(r)
			if token == "" {
				token = r.URL.Query().Get("token")
			}
			if token == "" {
				logger.Warn("auth: missing token",
					slog.String("path", r.URL.Path),
					slog.String("request_id", RequestIDFromContext(r.Context())),
				)
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error":   "unauthorized",
					"message": "missing or invalid authentication token",
				})
				return
			}

			// Verify the token.
			user, err := verifier.Verify(r.Context(), token)
			if err != nil {
				logger.Warn("auth: token verification failed",
					slog.String("error", err.Error()),
					slog.String("path", r.URL.Path),
					slog.String("remote_addr", r.RemoteAddr),
					slog.String("request_id", RequestIDFromContext(r.Context())),
				)
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error":   "unauthorized",
					"message": "missing or invalid authentication token",
				})
				return
			}

			// Store authenticated user in context and proceed.
			ctx := auth.NewContext(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// publicPaths lists routes that do not require authentication.
var publicPaths = map[string]bool{
	"/api/v1/health":  true,
	"/api/v1/version": true,
}

// isPublicPath checks if the given path is exempt from authentication.
func isPublicPath(path string) bool {
	return publicPaths[path]
}

// extractBearerToken extracts the token from the Authorization: Bearer <token> header.
func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return ""
	}
	return authHeader[len(prefix):]
}
