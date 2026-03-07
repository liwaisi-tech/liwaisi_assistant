package theme

import (
	"testing"
)

func TestGet_RegisteredThemes(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"dark theme exists", "dark", false},
		{"light theme exists", "light", false},
		{"unknown theme errors", "neon", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th, err := Get(tt.want)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Get(%q) error = %v, wantErr %v", tt.want, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if th.Name() != tt.want {
				t.Errorf("Name() = %q, want %q", th.Name(), tt.want)
			}
		})
	}
}

func TestNames(t *testing.T) {
	names := Names()
	if len(names) < 2 {
		t.Fatalf("Names() returned %d themes, want at least 2", len(names))
	}

	has := make(map[string]bool)
	for _, n := range names {
		has[n] = true
	}

	for _, want := range []string{"dark", "light"} {
		if !has[want] {
			t.Errorf("Names() missing %q, got %v", want, names)
		}
	}
}

func TestDarkTheme_Styles(t *testing.T) {
	th, err := Get("dark")
	if err != nil {
		t.Fatalf("Get(dark): %v", err)
	}

	styles := []struct {
		name string
		fn   func() any
	}{
		{"UserMessage", func() any { return th.UserMessage() }},
		{"AssistantMessage", func() any { return th.AssistantMessage() }},
		{"HeaderBar", func() any { return th.HeaderBar() }},
		{"StatusBar", func() any { return th.StatusBar() }},
		{"InputArea", func() any { return th.InputArea() }},
		{"Border", func() any { return th.Border() }},
		{"SuccessText", func() any { return th.SuccessText() }},
		{"ErrorText", func() any { return th.ErrorText() }},
		{"MutedText", func() any { return th.MutedText() }},
		{"ToolCallText", func() any { return th.ToolCallText() }},
		{"CommandText", func() any { return th.CommandText() }},
		{"GlamourStyle", func() any { return th.GlamourStyle() }},
	}

	for _, s := range styles {
		t.Run(s.name, func(t *testing.T) {
			result := s.fn()
			if result == nil {
				t.Errorf("%s() returned nil", s.name)
			}
		})
	}
}

func TestLightTheme_Styles(t *testing.T) {
	th, err := Get("light")
	if err != nil {
		t.Fatalf("Get(light): %v", err)
	}

	styles := []struct {
		name string
		fn   func() any
	}{
		{"UserMessage", func() any { return th.UserMessage() }},
		{"AssistantMessage", func() any { return th.AssistantMessage() }},
		{"HeaderBar", func() any { return th.HeaderBar() }},
		{"StatusBar", func() any { return th.StatusBar() }},
		{"InputArea", func() any { return th.InputArea() }},
		{"Border", func() any { return th.Border() }},
		{"SuccessText", func() any { return th.SuccessText() }},
		{"ErrorText", func() any { return th.ErrorText() }},
		{"MutedText", func() any { return th.MutedText() }},
		{"ToolCallText", func() any { return th.ToolCallText() }},
		{"CommandText", func() any { return th.CommandText() }},
		{"GlamourStyle", func() any { return th.GlamourStyle() }},
	}

	for _, s := range styles {
		t.Run(s.name, func(t *testing.T) {
			result := s.fn()
			if result == nil {
				t.Errorf("%s() returned nil", s.name)
			}
		})
	}
}
