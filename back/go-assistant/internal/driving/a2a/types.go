// Package a2a implements the A2A (Agent-to-Agent) protocol driving adapter.
// It maps between CPN domain types and A2A wire format types, providing
// JSON-RPC 2.0 transport with SSE streaming for agent interoperability.
//
// This package defines local wire format types instead of importing the
// official A2A SDK to avoid external dependency management issues.
package a2a

import "encoding/json"

// ── Task State Constants ───────────────────────────────────────────────────

const (
	// TaskStateSubmitted indicates the task has been received but not started.
	TaskStateSubmitted = "submitted"

	// TaskStateWorking indicates the task is actively being processed.
	TaskStateWorking = "working"

	// TaskStateInputRequired indicates the task is blocked waiting for user input.
	TaskStateInputRequired = "input-required"

	// TaskStateCompleted indicates the task finished successfully.
	TaskStateCompleted = "completed"

	// TaskStateFailed indicates the task terminated with an error.
	TaskStateFailed = "failed"

	// TaskStateCanceled indicates the task was canceled by the user.
	TaskStateCanceled = "canceled"
)

// ── Message Role Constants ─────────────────────────────────────────────────

const (
	// RoleUser indicates a message from the human user.
	RoleUser = "user"

	// RoleAgent indicates a message from the agent.
	RoleAgent = "agent"
)

// ── JSON-RPC Method Names ──────────────────────────────────────────────────

const (
	// MethodSendMessage is the JSON-RPC method for synchronous message sending.
	MethodSendMessage = "message/send"

	// MethodStreamMessage is the JSON-RPC method for streaming message sending.
	MethodStreamMessage = "message/stream"

	// MethodGetTask retrieves a task by ID.
	MethodGetTask = "tasks/get"

	// MethodListTasks lists tasks for the authenticated user.
	MethodListTasks = "tasks/list"

	// MethodCancelTask cancels a running task.
	MethodCancelTask = "tasks/cancel"
)

// ── JSON-RPC 2.0 Wire Types ───────────────────────────────────────────────

// JSONRPCRequest is a JSON-RPC 2.0 request envelope.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response envelope.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeTaskNotFound   = -32001
)

// ── A2A Task Types ─────────────────────────────────────────────────────────

// Task represents an A2A task — a unit of work mapped to a CPN session execution.
type Task struct {
	ID        string      `json:"id"`
	ContextID string      `json:"contextId"`
	Status    TaskStatus  `json:"status"`
	History   []Message   `json:"history,omitempty"`
	Artifacts []Artifact  `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus represents the current status of a task.
type TaskStatus struct {
	State     string   `json:"state"`
	Message   *Message `json:"message,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
}

// Message is an A2A message containing one or more parts.
type Message struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part is a content unit within an A2A message.
// Only one of Text, Data, or Raw should be set per part.
type Part struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	MimeType string         `json:"mimeType,omitempty"`
	Raw      string         `json:"raw,omitempty"`
}

// Artifact is an output produced by a task, composed of parts.
type Artifact struct {
	Name      string `json:"name,omitempty"`
	Parts     []Part `json:"parts"`
	Index     int    `json:"index"`
	Append    bool   `json:"append,omitempty"`
	LastChunk bool   `json:"lastChunk,omitempty"`
}

// ── A2A SSE Event Types ────────────────────────────────────────────────────

// TaskStatusUpdateEvent is an SSE event carrying a task status change.
type TaskStatusUpdateEvent struct {
	ID        string     `json:"id"`
	ContextID string     `json:"contextId"`
	Status    TaskStatus `json:"status"`
	Final     bool       `json:"final"`
}

// TaskArtifactUpdateEvent is an SSE event carrying an artifact delta.
type TaskArtifactUpdateEvent struct {
	ID        string   `json:"id"`
	ContextID string   `json:"contextId"`
	Artifact  Artifact `json:"artifact"`
}

// ── A2A Request Params ─────────────────────────────────────────────────────

// SendMessageRequest is the params for message/send and message/stream.
type SendMessageRequest struct {
	Message   Message        `json:"message"`
	TaskID    string         `json:"taskId,omitempty"`
	ContextID string         `json:"contextId,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// GetTaskRequest is the params for tasks/get.
type GetTaskRequest struct {
	TaskID string `json:"id"`
}

// ListTasksRequest is the params for tasks/list.
type ListTasksRequest struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// CancelTaskRequest is the params for tasks/cancel.
type CancelTaskRequest struct {
	TaskID string `json:"id"`
}

// ── AgentCard Types ────────────────────────────────────────────────────────

// AgentCard is the JSON metadata document describing an agent's capabilities.
type AgentCard struct {
	Name                 string              `json:"name"`
	Description          string              `json:"description"`
	Version              string              `json:"version"`
	Provider             AgentProvider       `json:"provider"`
	SupportedInterfaces  []AgentInterface    `json:"supportedInterfaces"`
	Capabilities         AgentCapabilities   `json:"capabilities"`
	SecuritySchemes      map[string]any      `json:"securitySchemes"`
	SecurityRequirements [][]string          `json:"securityRequirements"`
	DefaultInputModes    []string            `json:"defaultInputModes"`
	DefaultOutputModes   []string            `json:"defaultOutputModes"`
	Skills               []AgentSkill        `json:"skills"`
}

// AgentProvider describes the organization behind the agent.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url"`
}

// AgentInterface describes a supported protocol binding.
type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
}

// AgentCapabilities declares what the agent supports.
type AgentCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
	ExtendedAgentCard bool `json:"extendedAgentCard"`
}

// AgentSkill describes a capability of the agent.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}
