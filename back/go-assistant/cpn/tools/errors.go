package tools

import "errors"

// RegistryErrorCode is the stable machine-readable tag surfaced by
// RegistryError. The admin REST layer maps these codes 1:1 to HTTP statuses.
type RegistryErrorCode string

const (
	CodeDuplicate     RegistryErrorCode = "ErrDuplicate"
	CodeNotFound      RegistryErrorCode = "ErrNotFound"
	CodeSchemaInvalid RegistryErrorCode = "ErrSchemaInvalid"
	CodeBinaryDrift   RegistryErrorCode = "ErrBinaryDrift"
	CodeInvalidInput  RegistryErrorCode = "ErrInvalidInput"
	CodeForbidden     RegistryErrorCode = "ErrForbidden"
)

// RegistryError is the structured error returned by Registry mutations (PAT-001).
// Callers compare via errors.Is against the sentinel values below.
type RegistryError struct {
	Code    RegistryErrorCode
	Message string
	Err     error
}

func (e *RegistryError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.Err.Error()
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

func (e *RegistryError) Unwrap() error { return e.Err }

// Is supports errors.Is(err, ErrDuplicate) when err wraps a RegistryError
// whose Code matches the target's Code.
func (e *RegistryError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	other, ok := target.(*RegistryError)
	if !ok {
		return false
	}
	return e.Code == other.Code
}

var (
	// ErrDuplicate is returned when registering a tool with a
	// (namespace, name, version) triple that already exists.
	ErrDuplicate = &RegistryError{Code: CodeDuplicate, Message: "tool already registered"}

	// ErrNotFound is returned when a qualified name cannot be resolved.
	ErrNotFound = &RegistryError{Code: CodeNotFound, Message: "tool not found"}

	// ErrSchemaInvalid is returned when the JSON schema on a ToolEntry
	// cannot be parsed as JSON Schema draft 2020-12 (shape-only validation).
	ErrSchemaInvalid = &RegistryError{Code: CodeSchemaInvalid, Message: "tool schema invalid"}

	// ErrBinaryDrift is returned when invoking a binary-backed tool whose
	// on-disk SHA-256 no longer matches the manifest.
	ErrBinaryDrift = &RegistryError{Code: CodeBinaryDrift, Message: "binary sha256 mismatch"}

	// ErrInvalidInput is returned on malformed ToolEntry input.
	ErrInvalidInput = &RegistryError{Code: CodeInvalidInput, Message: "invalid input"}

	// ErrForbidden is returned when a protected origin is being mutated
	// (e.g. trying to hard-delete a builtin/user tool).
	ErrForbidden = &RegistryError{Code: CodeForbidden, Message: "operation forbidden"}

	// ── Legacy sentinels (pre-GAP-3). Kept so existing tests compile. ────

	// ErrToolAlreadyRegistered is the legacy sentinel for boot-time
	// duplicate registrations.
	ErrToolAlreadyRegistered = errors.New("tool already registered")

	// ErrSystemNamespaceSealed is returned when attempting to register a
	// system-namespace tool after the registry has been sealed.
	ErrSystemNamespaceSealed = errors.New("system namespace is sealed")

	// ErrInvalidToolSchema is returned when a ToolSchema is nil or has
	// empty required fields (Name, Namespace).
	ErrInvalidToolSchema = errors.New("invalid tool schema")

	// ErrToolNotFound is the legacy sentinel for resolve misses.
	ErrToolNotFound = errors.New("tool not found")
)

// wrapErr keeps the sentinel Code while attaching context.
func wrapErr(base *RegistryError, msg string, err error) *RegistryError {
	return &RegistryError{Code: base.Code, Message: msg, Err: err}
}
