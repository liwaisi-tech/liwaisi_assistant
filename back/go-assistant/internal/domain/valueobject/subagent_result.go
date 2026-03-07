package valueobject

import "time"

// SubAgentUsage tracks token consumption for a subagent execution.
type SubAgentUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// SubAgentResult holds the output from a subagent execution.
type SubAgentResult struct {
	AgentName string         `json:"agent_name"`
	Output    string         `json:"output"`
	Err       error          `json:"-"`
	Status    SubAgentStatus `json:"status"`
	Usage     SubAgentUsage  `json:"usage"`
	Elapsed   time.Duration  `json:"elapsed"`
}

// IsSuccess reports whether the subagent completed without error.
func (r *SubAgentResult) IsSuccess() bool {
	return r.Err == nil && r.Status == SubAgentStatusCompleted
}
