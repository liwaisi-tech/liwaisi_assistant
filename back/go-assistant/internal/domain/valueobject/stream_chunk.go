package valueobject

import "time"

// ToolEventKind distinguishes between different phases of the tool loop.
type ToolEventKind string

const (
	// ToolEventThinking indicates the LLM is deciding which tool to call.
	ToolEventThinking ToolEventKind = "thinking"
	// ToolEventCalling indicates a specific tool is being executed.
	ToolEventCalling ToolEventKind = "calling"
	// ToolEventToolLoaded indicates a tool category was loaded via find_tools.
	ToolEventToolLoaded ToolEventKind = "tool_loaded"
	// ToolEventSkillActivated indicates a skill was activated via find_skills.
	ToolEventSkillActivated ToolEventKind = "skill_activated"

	// ToolEventSubAgentStarted indicates a sub-agent has been spawned.
	ToolEventSubAgentStarted ToolEventKind = "subagent_started"
	// ToolEventSubAgentThinking indicates a sub-agent's LLM turn is starting.
	ToolEventSubAgentThinking ToolEventKind = "subagent_thinking"
	// ToolEventSubAgentToolCall indicates a sub-agent is executing a tool.
	ToolEventSubAgentToolCall ToolEventKind = "subagent_tool_call"
	// ToolEventSubAgentCompleted indicates a sub-agent has finished execution.
	ToolEventSubAgentCompleted ToolEventKind = "subagent_completed"
)

// ToolEvent carries real-time progress information from the tool execution loop.
type ToolEvent struct {
	Kind      ToolEventKind
	Iteration int
	Total     int
	ToolName  string
	Elapsed   time.Duration
	// Detail carries extra context for informational events (e.g., category
	// name, tool count, skill name).
	Detail string
}

// StreamChunk represents a single piece of a streaming response.
// During the tool loop phase, ToolEvent is non-nil and Content is empty.
// During the streaming phase, Content carries token data.
type StreamChunk struct {
	Content   string
	Done      bool
	Err       error
	ToolEvent *ToolEvent
}
