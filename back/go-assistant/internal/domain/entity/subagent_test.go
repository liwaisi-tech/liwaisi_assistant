package entity

import (
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestSubAgentSpec_EffectiveMaxTurns(t *testing.T) {
	tests := []struct {
		name     string
		maxTurns int
		want     int
	}{
		{"zero returns default 10", 0, 10},
		{"positive value returned as-is", 5, 5},
		{"large value returned as-is", 100, 100},
		{"one returned as-is", 1, 1},
		{"negative returns default 10", -5, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := SubAgentSpec{MaxTurns: tt.maxTurns}
			if got := spec.EffectiveMaxTurns(); got != tt.want {
				t.Errorf("EffectiveMaxTurns() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSubAgentSpec_HasToolRestrictions(t *testing.T) {
	tests := []struct {
		name         string
		allowedTools []string
		deniedTools  []string
		want         bool
	}{
		{"no restrictions", nil, nil, false},
		{"empty slices", []string{}, []string{}, false},
		{"allowed tools only", []string{"read_file"}, nil, true},
		{"denied tools only", nil, []string{"execute_command"}, true},
		{"both allowed and denied", []string{"read_file"}, []string{"execute_command"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := SubAgentSpec{
				AllowedTools: tt.allowedTools,
				DeniedTools:  tt.deniedTools,
			}
			if got := spec.HasToolRestrictions(); got != tt.want {
				t.Errorf("HasToolRestrictions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSubAgentSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    SubAgentSpec
		wantErr bool
	}{
		{
			name: "valid minimal spec",
			spec: SubAgentSpec{
				Name:        "reviewer",
				Instruction: "Review code for security issues",
			},
			wantErr: false,
		},
		{
			name: "valid full spec",
			spec: SubAgentSpec{
				Name:         "security-reviewer",
				Description:  "Reviews code for security vulnerabilities",
				Instruction:  "You are a security expert. Review the code.",
				Context:      "File: main.go\nContent: ...",
				AllowedTools: []string{"read_file", "grep"},
				DeniedTools:  []string{"execute_command"},
				ModelTier:    valueobject.ModelTierCapable,
				MaxTurns:     5,
			},
			wantErr: false,
		},
		{
			name: "empty model tier is valid",
			spec: SubAgentSpec{
				Name:        "explorer",
				Instruction: "Explore the codebase",
				ModelTier:   "",
			},
			wantErr: false,
		},
		{
			name: "empty name",
			spec: SubAgentSpec{
				Name:        "",
				Instruction: "Review code",
			},
			wantErr: true,
		},
		{
			name: "empty instruction",
			spec: SubAgentSpec{
				Name:        "reviewer",
				Instruction: "",
			},
			wantErr: true,
		},
		{
			name: "negative max turns",
			spec: SubAgentSpec{
				Name:        "reviewer",
				Instruction: "Review code",
				MaxTurns:    -1,
			},
			wantErr: true,
		},
		{
			name: "invalid model tier",
			spec: SubAgentSpec{
				Name:        "reviewer",
				Instruction: "Review code",
				ModelTier:   valueobject.ModelTier("turbo"),
			},
			wantErr: true,
		},
		{
			name: "zero max turns is valid",
			spec: SubAgentSpec{
				Name:        "reviewer",
				Instruction: "Review code",
				MaxTurns:    0,
			},
			wantErr: false,
		},
		{
			name: "empty string in allowed tools",
			spec: SubAgentSpec{
				Name:         "reviewer",
				Instruction:  "Review code",
				AllowedTools: []string{"read_file", ""},
			},
			wantErr: true,
		},
		{
			name: "empty string in denied tools",
			spec: SubAgentSpec{
				Name:        "reviewer",
				Instruction: "Review code",
				DeniedTools: []string{"", "execute_command"},
			},
			wantErr: true,
		},
		{
			name: "tool in both allowed and denied",
			spec: SubAgentSpec{
				Name:         "reviewer",
				Instruction:  "Review code",
				AllowedTools: []string{"read_file", "grep"},
				DeniedTools:  []string{"read_file"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
