package cli

import (
	"sort"

	tea "charm.land/bubbletea/v2"
)

// CommandHandler processes a slash command and returns a BubbleTea command.
type CommandHandler func(args string) tea.Cmd

// CommandEntry describes a registered slash command.
type CommandEntry struct {
	Name        string
	Description string
	Handler     CommandHandler
}

// CommandRegistry maps slash command names to handlers.
type CommandRegistry struct {
	commands map[string]CommandEntry
}

// NewCommandRegistry creates an empty CommandRegistry.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]CommandEntry),
	}
}

// Register adds a command entry to the registry.
func (r *CommandRegistry) Register(entry CommandEntry) {
	r.commands[entry.Name] = entry
}

// Execute looks up and runs the handler for the named command.
// Returns the tea.Cmd and true if found, nil and false otherwise.
func (r *CommandRegistry) Execute(name, args string) (tea.Cmd, bool) {
	entry, ok := r.commands[name]
	if !ok {
		return nil, false
	}
	return entry.Handler(args), true
}

// Has reports whether a command with the given name is registered.
func (r *CommandRegistry) Has(name string) bool {
	_, ok := r.commands[name]
	return ok
}

// Entries returns all registered commands sorted by name.
func (r *CommandRegistry) Entries() []CommandEntry {
	entries := make([]CommandEntry, 0, len(r.commands))
	for _, e := range r.commands {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}
