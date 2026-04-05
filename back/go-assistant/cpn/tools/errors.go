package tools

import "errors"

var (
	// ErrToolAlreadyRegistered is returned when registering a tool with a
	// qualified name that already exists in the registry.
	ErrToolAlreadyRegistered = errors.New("tool already registered")

	// ErrSystemNamespaceSealed is returned when attempting to register a
	// system-namespace tool after the registry has been sealed.
	ErrSystemNamespaceSealed = errors.New("system namespace is sealed")

	// ErrInvalidToolSchema is returned when a ToolSchema is nil or has
	// empty required fields (Name, Namespace).
	ErrInvalidToolSchema = errors.New("invalid tool schema")

	// ErrToolNotFound is returned when a qualified name cannot be resolved.
	ErrToolNotFound = errors.New("tool not found")
)
