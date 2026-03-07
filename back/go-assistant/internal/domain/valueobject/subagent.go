package valueobject

import "fmt"

// ModelTier represents the cost/capability tier for model selection.
type ModelTier string

const (
	// ModelTierFast selects a cheap, low-latency model (e.g., Haiku).
	ModelTierFast ModelTier = "fast"
	// ModelTierBalanced selects a mid-range model (e.g., Sonnet).
	ModelTierBalanced ModelTier = "balanced"
	// ModelTierCapable selects the highest-capability model (e.g., Opus).
	ModelTierCapable ModelTier = "capable"
)

var validModelTiers = map[ModelTier]struct{}{
	ModelTierFast:     {},
	ModelTierBalanced: {},
	ModelTierCapable:  {},
}

// String returns the string representation of the ModelTier.
func (m ModelTier) String() string {
	return string(m)
}

// IsValid reports whether the ModelTier is a known valid tier.
func (m ModelTier) IsValid() bool {
	_, ok := validModelTiers[m]
	return ok
}

// Validate returns an error if the ModelTier is not valid.
func (m ModelTier) Validate() error {
	if !m.IsValid() {
		return fmt.Errorf("invalid model tier: %q", string(m))
	}
	return nil
}

// SubAgentStatus represents the lifecycle state of a subagent execution.
type SubAgentStatus string

const (
	// SubAgentStatusPending means the subagent is queued but not yet running.
	SubAgentStatusPending SubAgentStatus = "pending"
	// SubAgentStatusRunning means the subagent is actively executing.
	SubAgentStatusRunning SubAgentStatus = "running"
	// SubAgentStatusCompleted means the subagent finished successfully.
	SubAgentStatusCompleted SubAgentStatus = "completed"
	// SubAgentStatusFailed means the subagent encountered an error.
	SubAgentStatusFailed SubAgentStatus = "failed"
	// SubAgentStatusCanceled means the subagent was canceled before completion.
	SubAgentStatusCanceled SubAgentStatus = "canceled"
	// SubAgentStatusMaxTurns means the subagent was force-stopped because it
	// exhausted its maximum turn budget while tool calls were still pending.
	SubAgentStatusMaxTurns SubAgentStatus = "max_turns_exhausted"
)

var validSubAgentStatuses = map[SubAgentStatus]struct{}{
	SubAgentStatusPending:   {},
	SubAgentStatusRunning:   {},
	SubAgentStatusCompleted: {},
	SubAgentStatusFailed:    {},
	SubAgentStatusCanceled:  {},
	SubAgentStatusMaxTurns:  {},
}

var terminalStatuses = map[SubAgentStatus]struct{}{
	SubAgentStatusCompleted: {},
	SubAgentStatusFailed:    {},
	SubAgentStatusCanceled:  {},
	SubAgentStatusMaxTurns:  {},
}

// String returns the string representation of the SubAgentStatus.
func (s SubAgentStatus) String() string {
	return string(s)
}

// IsValid reports whether the SubAgentStatus is a known valid status.
func (s SubAgentStatus) IsValid() bool {
	_, ok := validSubAgentStatuses[s]
	return ok
}

// IsTerminal reports whether the status represents a final state
// (completed, failed, or canceled).
func (s SubAgentStatus) IsTerminal() bool {
	_, ok := terminalStatuses[s]
	return ok
}

// Validate returns an error if the SubAgentStatus is not valid.
func (s SubAgentStatus) Validate() error {
	if !s.IsValid() {
		return fmt.Errorf("invalid subagent status: %q", string(s))
	}
	return nil
}
