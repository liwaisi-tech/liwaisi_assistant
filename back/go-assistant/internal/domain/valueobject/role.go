// Package valueobject contains domain value objects.
package valueobject

import "fmt"

// Role represents the sender role in a chat message.
type Role string

const (
	// RoleSystem is the system prompt role.
	RoleSystem Role = "system"
	// RoleUser is the human user role.
	RoleUser Role = "user"
	// RoleAssistant is the AI assistant role.
	RoleAssistant Role = "assistant"
	// RoleTool is the tool result role.
	RoleTool Role = "tool"
)

var validRoles = map[Role]struct{}{
	RoleSystem:    {},
	RoleUser:      {},
	RoleAssistant: {},
	RoleTool:      {},
}

// String returns the string representation of the role.
func (r Role) String() string {
	return string(r)
}

// IsValid reports whether the role is a known valid role.
func (r Role) IsValid() bool {
	_, ok := validRoles[r]
	return ok
}

// Validate returns an error if the role is not valid.
func (r Role) Validate() error {
	if !r.IsValid() {
		return fmt.Errorf("invalid role: %q", string(r))
	}
	return nil
}
