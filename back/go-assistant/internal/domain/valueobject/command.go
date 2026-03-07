package valueobject

import "strings"

// SlashCommand represents a parsed slash command from user input.
type SlashCommand struct {
	Name string
	Args string
}

// ParseSlashCommand parses a string starting with "/" into a SlashCommand.
// Returns (cmd, true) if valid, (zero, false) if not a command.
func ParseSlashCommand(input string) (SlashCommand, bool) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return SlashCommand{}, false
	}

	body := input[1:]
	if body == "" {
		return SlashCommand{}, false
	}

	parts := strings.SplitN(body, " ", 2)
	cmd := SlashCommand{Name: strings.ToLower(parts[0])}
	if len(parts) > 1 {
		cmd.Args = strings.TrimSpace(parts[1])
	}

	return cmd, true
}

// IsSlashCommand reports whether the input starts with "/".
func IsSlashCommand(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), "/")
}
