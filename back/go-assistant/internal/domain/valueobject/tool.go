package valueobject

import "encoding/json"

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID       string
	Type     string // always "function"
	Function FunctionCall
}

// FunctionCall holds the function name and its JSON-encoded arguments.
type FunctionCall struct {
	Name      string
	Arguments string // JSON-encoded
}

// ToolDefinition describes a tool that the LLM can call.
type ToolDefinition struct {
	Type     string // always "function"
	Function FunctionDefinition
}

// FunctionDefinition describes the function's name, purpose, and parameter schema.
type FunctionDefinition struct {
	Name        string
	Description string
	Parameters  json.RawMessage // JSON Schema
}

// ToolChoice controls how the LLM selects tools.
type ToolChoice string

const (
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceRequired ToolChoice = "required"
)
