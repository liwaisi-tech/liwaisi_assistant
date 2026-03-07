// Package shellexec provides sandboxed OS command execution tools for the agent.
package shellexec

import (
	"fmt"
	"log/slog"
	"strings"
)

const defaultMaxCommandLength = 8192

// blockedPattern represents a pattern to match against command tokens.
type blockedPattern struct {
	// tokens is the sequence of tokens that must appear at the start of a subcommand.
	// A single-element slice matches the command name (e.g., ["sudo"]).
	// Multi-element slices match a command prefix (e.g., ["rm", "-rf", "/"]).
	tokens []string
	// description explains why this pattern is blocked.
	description string
}

// CommandPolicy defines the security policy for command execution.
type CommandPolicy struct {
	blocked          []blockedPattern
	pipeTargets      map[string]bool
	maxCommandLength int
}

// PolicyOption configures a CommandPolicy.
type PolicyOption func(*CommandPolicy)

// WithBlockedCommands adds additional command names to the blocklist.
func WithBlockedCommands(cmds ...string) PolicyOption {
	return func(p *CommandPolicy) {
		for _, cmd := range cmds {
			p.blocked = append(p.blocked, blockedPattern{
				tokens:      []string{cmd},
				description: "custom blocked command",
			})
		}
	}
}

// WithMaxCommandLength overrides the maximum allowed command length.
func WithMaxCommandLength(n int) PolicyOption {
	return func(p *CommandPolicy) {
		if n > 0 {
			p.maxCommandLength = n
		}
	}
}

// NewDefaultPolicy creates a CommandPolicy with sensible security defaults.
func NewDefaultPolicy(opts ...PolicyOption) *CommandPolicy {
	p := &CommandPolicy{
		maxCommandLength: defaultMaxCommandLength,
		blocked: []blockedPattern{
			{tokens: []string{"sudo"}, description: "privilege escalation"},
			{tokens: []string{"su"}, description: "user switching"},
			{tokens: []string{"doas"}, description: "privilege escalation"},
			{tokens: []string{"rm", "-rf", "/"}, description: "recursive root deletion"},
			{tokens: []string{"rm", "-rf", "/*"}, description: "recursive root deletion"},
			{tokens: []string{"rm", "-fr", "/"}, description: "recursive root deletion"},
			{tokens: []string{"rm", "-fr", "/*"}, description: "recursive root deletion"},
			{tokens: []string{"mkfs"}, description: "filesystem formatting"},
			{tokens: []string{"mkfs.ext4"}, description: "filesystem formatting"},
			{tokens: []string{"mkfs.xfs"}, description: "filesystem formatting"},
			{tokens: []string{"mkfs.btrfs"}, description: "filesystem formatting"},
			{tokens: []string{"dd"}, description: "raw disk operations"},
			{tokens: []string{"shutdown"}, description: "system shutdown"},
			{tokens: []string{"reboot"}, description: "system reboot"},
			{tokens: []string{"halt"}, description: "system halt"},
			{tokens: []string{"poweroff"}, description: "system poweroff"},
			{tokens: []string{"init"}, description: "init system control"},
			{tokens: []string{"systemctl", "poweroff"}, description: "system poweroff"},
			{tokens: []string{"systemctl", "reboot"}, description: "system reboot"},
			{tokens: []string{"systemctl", "halt"}, description: "system halt"},
			{tokens: []string{"chmod", "-R", "777"}, description: "recursive world-writable permissions"},
			{tokens: []string{"chown", "-R"}, description: "recursive ownership change"},
			{tokens: []string{"crontab", "-r"}, description: "crontab removal"},
			{tokens: []string{"iptables", "-F"}, description: "firewall flush"},
			{tokens: []string{"nft", "flush"}, description: "firewall flush"},
			{tokens: []string{"eval"}, description: "arbitrary code execution via eval"},
			{tokens: []string{"bash", "-c"}, description: "arbitrary code execution via bash -c"},
			{tokens: []string{"sh", "-c"}, description: "arbitrary code execution via sh -c"},
			{tokens: []string{"zsh", "-c"}, description: "arbitrary code execution via zsh -c"},
			{tokens: []string{"dash", "-c"}, description: "arbitrary code execution via dash -c"},
			{tokens: []string{"ksh", "-c"}, description: "arbitrary code execution via ksh -c"},
			{tokens: []string{"fish", "-c"}, description: "arbitrary code execution via fish -c"},
		},
		pipeTargets: map[string]bool{
			"sh":   true,
			"bash": true,
			"zsh":  true,
			"dash": true,
			"fish": true,
			"ksh":  true,
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Validate checks whether a command is allowed by the policy.
// It splits compound commands and validates each subcommand independently.
func (p *CommandPolicy) Validate(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return fmt.Errorf("command is empty")
	}
	if len(trimmed) > p.maxCommandLength {
		return fmt.Errorf("command exceeds maximum length of %d characters", p.maxCommandLength)
	}
	if strings.Contains(trimmed, ":(){ :|:& };:") {
		return fmt.Errorf("command blocked: fork bomb")
	}
	if containsCommandSubstitution(trimmed) {
		return fmt.Errorf("command blocked: command substitution ($() or backticks) is not allowed")
	}

	parts := splitCompoundCommand(trimmed)
	for i, part := range parts {
		tokens := tokenize(part)
		if len(tokens) == 0 {
			continue
		}
		if err := p.checkBlocked(tokens); err != nil {
			slog.Debug("command blocked by policy", "command", command, "reason", err.Error())
			return err
		}
		if i > 0 {
			if err := p.checkPipeTarget(tokens); err != nil {
				slog.Debug("command blocked by policy", "command", command, "reason", err.Error())
				return err
			}
		}
	}
	return nil
}

// checkBlocked tests tokens against the blocklist patterns.
func (p *CommandPolicy) checkBlocked(tokens []string) error {
	for _, bp := range p.blocked {
		if matchesPrefix(tokens, bp.tokens) {
			return fmt.Errorf("command blocked: %s (%s)", bp.tokens[0], bp.description)
		}
	}
	return nil
}

// checkPipeTarget blocks piping into shell interpreters (e.g., curl ... | sh).
func (p *CommandPolicy) checkPipeTarget(tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	cmd := tokens[0]
	if p.pipeTargets[cmd] {
		return fmt.Errorf("command blocked: piping into %s is not allowed", cmd)
	}
	return nil
}

// matchesPrefix reports whether tokens starts with the given prefix sequence.
func matchesPrefix(tokens, prefix []string) bool {
	if len(tokens) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if tokens[i] != p {
			return false
		}
	}
	return true
}

// splitCompoundCommand splits a command string on unquoted compound operators
// (&&, ||, ;, |). It respects single and double quotes.
func splitCompoundCommand(cmd string) []string {
	var parts []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if ch == '\\' && !inSingle && i+1 < len(runes) {
			current.WriteRune(ch)
			i++
			current.WriteRune(runes[i])
			continue
		}

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(ch)
			continue
		}

		if inSingle || inDouble {
			current.WriteRune(ch)
			continue
		}

		switch {
		case ch == '|' && i+1 < len(runes) && runes[i+1] == '|':
			// ||
			s := strings.TrimSpace(current.String())
			if s != "" {
				parts = append(parts, s)
			}
			current.Reset()
			i++
		case ch == '&' && i+1 < len(runes) && runes[i+1] == '&':
			// &&
			s := strings.TrimSpace(current.String())
			if s != "" {
				parts = append(parts, s)
			}
			current.Reset()
			i++
		case ch == '&':
			// single & (background operator, acts as command terminator)
			s := strings.TrimSpace(current.String())
			if s != "" {
				parts = append(parts, s)
			}
			current.Reset()
		case ch == '|':
			// single pipe
			s := strings.TrimSpace(current.String())
			if s != "" {
				parts = append(parts, s)
			}
			current.Reset()
		case ch == ';':
			s := strings.TrimSpace(current.String())
			if s != "" {
				parts = append(parts, s)
			}
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}

	if s := strings.TrimSpace(current.String()); s != "" {
		parts = append(parts, s)
	}
	return parts
}

// tokenize splits a shell command into tokens, respecting single and double
// quotes. It strips surrounding quotes from tokens but preserves their content.
// Environment variable assignments (KEY=val) before the command are preserved
// as separate tokens.
func tokenize(cmd string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if ch == '\\' && !inSingle && i+1 < len(runes) {
			current.WriteRune(runes[i+1])
			i++
			continue
		}

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if inSingle || inDouble {
			current.WriteRune(ch)
			continue
		}

		if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(ch)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return skipEnvAssignments(tokens)
}

// skipEnvAssignments strips leading KEY=value tokens so the actual command name
// is checked against the blocklist (e.g., "GOOS=linux go build" -> ["go", "build"]).
func skipEnvAssignments(tokens []string) []string {
	for i, t := range tokens {
		if !isEnvAssignment(t) {
			return tokens[i:]
		}
	}
	return nil
}

// containsCommandSubstitution reports whether cmd contains command substitution
// syntax that would be expanded by the shell: $(...) or backticks.
// Only single quotes suppress expansion; double quotes do NOT prevent $() or
// backtick expansion in POSIX shell.
func containsCommandSubstitution(cmd string) bool {
	inSingle := false
	inDouble := false
	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if ch == '\\' && !inSingle && i+1 < len(runes) {
			i++
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle {
			continue
		}

		if ch == '$' && i+1 < len(runes) && runes[i+1] == '(' {
			return true
		}
		if ch == '`' {
			return true
		}
	}
	return false
}

// isEnvAssignment reports whether token looks like an environment variable
// assignment (e.g., "KEY=value"). The key must start with a letter or underscore
// and contain only alphanumerics and underscores.
func isEnvAssignment(token string) bool {
	eqIdx := strings.IndexByte(token, '=')
	if eqIdx <= 0 {
		return false
	}
	key := token[:eqIdx]
	for i, ch := range key {
		if i == 0 {
			if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '_') {
				return false
			}
		} else {
			if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_') {
				return false
			}
		}
	}
	return true
}
