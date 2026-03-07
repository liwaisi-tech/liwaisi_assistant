package valueobject

import "testing"

func TestRole_String(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want string
	}{
		{"system role", RoleSystem, "system"},
		{"user role", RoleUser, "user"},
		{"assistant role", RoleAssistant, "assistant"},
		{"tool role", RoleTool, "tool"},
		{"empty role", Role(""), ""},
		{"unknown role", Role("unknown"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.String(); got != tt.want {
				t.Errorf("Role.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRole_IsValid(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want bool
	}{
		{"system is valid", RoleSystem, true},
		{"user is valid", RoleUser, true},
		{"assistant is valid", RoleAssistant, true},
		{"tool is valid", RoleTool, true},
		{"empty is invalid", Role(""), false},
		{"unknown is invalid", Role("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.IsValid(); got != tt.want {
				t.Errorf("Role.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRole_Validate(t *testing.T) {
	tests := []struct {
		name    string
		role    Role
		wantErr bool
	}{
		{"system no error", RoleSystem, false},
		{"user no error", RoleUser, false},
		{"assistant no error", RoleAssistant, false},
		{"tool no error", RoleTool, false},
		{"empty returns error", Role(""), true},
		{"unknown returns error", Role("moderator"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.role.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Role.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
