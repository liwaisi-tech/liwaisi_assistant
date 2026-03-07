package valueobject

import "testing"

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd SlashCommand
		wantOk  bool
	}{
		{
			name:    "simple clear command",
			input:   "/clear",
			wantCmd: SlashCommand{Name: "clear"},
			wantOk:  true,
		},
		{
			name:    "command with args",
			input:   "/resume my-session",
			wantCmd: SlashCommand{Name: "resume", Args: "my-session"},
			wantOk:  true,
		},
		{
			name:    "command with multiple args",
			input:   "/resume my session with spaces",
			wantCmd: SlashCommand{Name: "resume", Args: "my session with spaces"},
			wantOk:  true,
		},
		{
			name:   "not a command",
			input:  "hello world",
			wantOk: false,
		},
		{
			name:   "empty input",
			input:  "",
			wantOk: false,
		},
		{
			name:   "just a slash",
			input:  "/",
			wantOk: false,
		},
		{
			name:    "command with leading whitespace",
			input:   "  /clear",
			wantCmd: SlashCommand{Name: "clear"},
			wantOk:  true,
		},
		{
			name:    "command is lowercased",
			input:   "/CLEAR",
			wantCmd: SlashCommand{Name: "clear"},
			wantOk:  true,
		},
		{
			name:    "mixed case command",
			input:   "/ReSuMe latest",
			wantCmd: SlashCommand{Name: "resume", Args: "latest"},
			wantOk:  true,
		},
		{
			name:    "command with trailing whitespace in args",
			input:   "/help   ",
			wantCmd: SlashCommand{Name: "help"},
			wantOk:  true,
		},
		{
			name:    "args with extra spaces trimmed",
			input:   "/resume   my-session  ",
			wantCmd: SlashCommand{Name: "resume", Args: "my-session"},
			wantOk:  true,
		},
		{
			name:    "rewind command",
			input:   "/rewind",
			wantCmd: SlashCommand{Name: "rewind"},
			wantOk:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, ok := ParseSlashCommand(tt.input)
			if ok != tt.wantOk {
				t.Fatalf("ParseSlashCommand(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
			}
			if ok && cmd != tt.wantCmd {
				t.Errorf("ParseSlashCommand(%q) = %+v, want %+v", tt.input, cmd, tt.wantCmd)
			}
		})
	}
}

func TestIsSlashCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"slash prefix", "/clear", true},
		{"slash only", "/", true},
		{"with leading space", "  /clear", true},
		{"no slash", "hello", false},
		{"empty", "", false},
		{"slash in middle", "not /a command", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSlashCommand(tt.input); got != tt.want {
				t.Errorf("IsSlashCommand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
