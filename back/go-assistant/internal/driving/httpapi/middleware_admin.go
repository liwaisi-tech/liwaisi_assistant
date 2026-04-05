package httpapi

import (
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// AdminMiddleware returns middleware that restricts access to the admin email.
// In dev-mode (Sub == "dev-user"), access is always granted.
func AdminMiddleware(adminEmail string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := auth.UserFromContext(r.Context())
			if user == nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error":   "unauthorized",
					"message": "authentication required",
				})
				return
			}

			// Dev-mode: always allow.
			if user.Sub == "dev-user" {
				next.ServeHTTP(w, r)
				return
			}

			if user.Email != adminEmail {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error":   "forbidden",
					"message": "admin access required",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
