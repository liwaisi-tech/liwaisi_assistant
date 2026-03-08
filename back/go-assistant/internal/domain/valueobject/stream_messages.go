package valueobject

// ClientMessage represents an incoming message from the client over a persistent stream.
type ClientMessage struct {
	Text       string      // Used when the user sends a message
	ToolResult *ToolResult // Used when the client completes a requested tool
}

// ServerMessage represents an outgoing message from the agent to the client.
type ServerMessage struct {
	Chunk    string    // Incremental text response
	ToolCall *ToolCall // Requesting the client to run a tool
	Error    error     // Error state
	Done     bool      // Indicates the end of the current response generation
}

// ToolResult is the result returned by a client-side tool execution.
type ToolResult struct {
	ID     string
	Result string
}
