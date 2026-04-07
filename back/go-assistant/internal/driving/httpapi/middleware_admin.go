package httpapi

import (
	"net/http"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// AdminMiddleware restricts access to users whose email matches any entry in adminEmails.
// Comparison is case-insensitive after trimming.
func AdminMiddleware(adminEmails []string) func(http.Handler) http.Handler {
	// Pre-normalize the configured set.
	normalized := make([]string, 0, len(adminEmails))
	for _, e := range adminEmails {
		e = strings.TrimSpace(e)
		if e != "" {
			normalized = append(normalized, e)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := auth.UserFromContext(r.Context())
			if user == nil {
				writeError(w, http.StatusForbidden, "admin access required")
				return
			}
			email := strings.TrimSpace(user.Email)
			if email == "" {
				writeError(w, http.StatusForbidden, "admin access required")
				return
			}
			for _, allowed := range normalized {
				if strings.EqualFold(email, allowed) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusForbidden, "admin access required")
		})
	}
}

// ParseAdminEmails is the exported wrapper for use by composition root.
func ParseAdminEmails(raw string) []string { return parseAdminEmails(raw) }

// parseAdminEmails parses a comma-separated env value into a normalized slice.
func parseAdminEmails(raw string) []string {
	out := make([]string, 0)
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}
