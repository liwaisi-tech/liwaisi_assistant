package prompts

import (
	"strings"
	"testing"
)

func TestIsSupported(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
		want bool
	}{
		{"es-CO ok", "es-CO", true},
		{"es-MX ok", "es-MX", true},
		{"es-AR ok", "es-AR", true},
		{"es-ES ok", "es-ES", true},
		{"en-GB ok", "en-GB", true},
		{"en-US ok", "en-US", true},
		{"en-AU ok", "en-AU", true},
		{"lowercase rejected", "es-co", false},
		{"unsupported language", "pt-BR", false},
		{"empty rejected", "", false},
		{"injection rejected", "es-CO'); DROP TABLE users;--", false},
		{"random rejected", "xx-YY", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsSupported(tc.code); got != tc.want {
				t.Errorf("IsSupported(%q) = %v, want %v", tc.code, got, tc.want)
			}
		})
	}
}

func TestDefaultVariant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		lang string
		want string
	}{
		{"es", "es-CO"},
		{"en", "en-GB"},
		{"", "es-CO"},
		{"pt", "es-CO"},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			t.Parallel()
			if got := DefaultVariant(tc.lang); got != tc.want {
				t.Errorf("DefaultVariant(%q) = %q, want %q", tc.lang, got, tc.want)
			}
		})
	}
}

func TestPreambleFor(t *testing.T) {
	t.Parallel()

	const header = "USER CONTEXT — REGIONAL REGISTER"
	const maxChars = 700 // generous bound; soft target ~320 (≤80 tokens).

	for code := range SupportedVariants {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			p := PreambleFor(code)
			if p == "" {
				t.Fatalf("PreambleFor(%q) returned empty", code)
			}
			if !strings.HasPrefix(p, header) {
				t.Errorf("preamble missing canonical header; got prefix %q", p[:min(len(p), 40)])
			}
			if len(p) > maxChars {
				t.Errorf("preamble too long: %d chars (max %d)", len(p), maxChars)
			}
		})
	}

	t.Run("unknown returns default", func(t *testing.T) {
		t.Parallel()
		got := PreambleFor("xx-YY")
		want := PreambleFor("es-CO")
		if got != want {
			t.Errorf("unknown variant should fall back to es-CO preamble")
		}
	})

	t.Run("empty returns default", func(t *testing.T) {
		t.Parallel()
		if PreambleFor("") != PreambleFor("es-CO") {
			t.Errorf("empty variant should fall back to es-CO preamble")
		}
	})
}

