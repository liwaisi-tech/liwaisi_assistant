package entity

import (
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	defaultMaxTurns = 10
	defaultTimeout  = 2 * time.Minute
)

// SubAgentSpec defines a subagent using the 4-tuple abstraction:
// <Instruction, Context, Tools, Model>.
type SubAgentSpec struct {
	Name         string                `json:"name"`
	BuiltIn      bool                  `json:"builtin,omitempty"` // New field
	Metadata     map[string]string     `json:"metadata,omitempty"` // New field
	Description  string                `json:"description"`
	Instruction  string                `json:"instruction"`
	Context      string                `json:"context"`
	AllowedTools []string              `json:"allowed_tools,omitempty"`
	DeniedTools  []string              `json:"denied_tools,omitempty"`
	ModelTier    valueobject.ModelTier `json:"model_tier,omitempty"`
	MaxTurns     int                   `json:"max_turns,omitempty"`
	Timeout      time.Duration         `json:"timeout,omitempty"`
}

// EffectiveMaxTurns returns MaxTurns if set, otherwise the default of 10.
func (s *SubAgentSpec) EffectiveMaxTurns() int {
	if s.MaxTurns > 0 {
		return s.MaxTurns
	}
	return defaultMaxTurns
}

// EffectiveTimeout returns Timeout if set, otherwise the default of 2 minutes.
func (s *SubAgentSpec) EffectiveTimeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return defaultTimeout
}

// HasToolRestrictions reports whether the subagent has any tool filtering configured.
func (s *SubAgentSpec) HasToolRestrictions() bool {
	return len(s.AllowedTools) > 0 || len(s.DeniedTools) > 0
}

// Validate checks that all required fields are present and constraints are met.
func (s *SubAgentSpec) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("subagent spec: name is required")
	}
	if s.Instruction == "" {
		return fmt.Errorf("subagent spec: instruction is required")
	}
	if s.MaxTurns < 0 {
		return fmt.Errorf("subagent spec: max_turns must be >= 0, got %d", s.MaxTurns)
	}
	if s.ModelTier != "" && !s.ModelTier.IsValid() {
		return fmt.Errorf("subagent spec: %w", s.ModelTier.Validate())
	}
	for _, t := range s.AllowedTools {
		if t == "" {
			return fmt.Errorf("subagent spec: allowed_tools contains empty string")
		}
	}
	for _, t := range s.DeniedTools {
		if t == "" {
			return fmt.Errorf("subagent spec: denied_tools contains empty string")
		}
	}
	if len(s.AllowedTools) > 0 && len(s.DeniedTools) > 0 {
		allowed := make(map[string]struct{}, len(s.AllowedTools))
		for _, t := range s.AllowedTools {
			allowed[t] = struct{}{}
		}
		for _, t := range s.DeniedTools {
			if _, ok := allowed[t]; ok {
				return fmt.Errorf("subagent spec: tool %q in both allowed and denied lists", t)
			}
		}
	}
	return nil
}
