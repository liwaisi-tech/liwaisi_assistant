package httpapi

import (
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// AdminMiddleware restricts access to users whose email matches adminEmail.
func AdminMiddleware(adminEmail string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := auth.UserFromContext(r.Context())
			if user == nil || user.Email != adminEmail {
				writeError(w, http.StatusForbidden, "admin access required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
