package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

func TestAdminMiddleware_TruthTable(t *testing.T) {
	admins := []string{"owner@liwaisi.tech", "ops@liwaisi.tech"}
	mw := AdminMiddleware(admins)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw(next)

	cases := []struct {
		name string
		user *auth.AuthenticatedUser
		want int
	}{
		{"nil user", nil, http.StatusForbidden},
		{"empty email", &auth.AuthenticatedUser{Email: ""}, http.StatusForbidden},
		{"intruder", &auth.AuthenticatedUser{Email: "intruder@example.com"}, http.StatusForbidden},
		{"exact owner", &auth.AuthenticatedUser{Email: "owner@liwaisi.tech"}, http.StatusOK},
		{"upper owner", &auth.AuthenticatedUser{Email: "OWNER@LIWAISI.TECH"}, http.StatusOK},
		{"padded ops", &auth.AuthenticatedUser{Email: "  ops@liwaisi.tech  "}, http.StatusOK},
		{"mixed case ops", &auth.AuthenticatedUser{Email: "ops@LIWAISI.tech"}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin", nil)
			if tc.user != nil {
				req = req.WithContext(auth.NewContext(req.Context(), tc.user))
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status=%d want=%d", rec.Code, tc.want)
			}
		})
	}
}

func TestParseAdminEmails(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{" , , ", 0},
		{"a@x.io", 1},
		{"a@x.io, b@x.io", 2},
		{"  A@x.io ,B@X.IO ", 2},
	}
	for _, c := range cases {
		got := parseAdminEmails(c.in)
		if len(got) != c.want {
			t.Errorf("parseAdminEmails(%q)=%v want len %d", c.in, got, c.want)
		}
	}
}
