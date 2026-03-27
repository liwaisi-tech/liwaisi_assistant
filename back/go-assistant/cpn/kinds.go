package cpn

// NodeKind identifies the type of a transition, determining its firing behavior.
type NodeKind string

const (
	// NodeKindTool represents a transition that invokes an external tool.
	NodeKindTool NodeKind = "tool"

	// NodeKindLLM represents a transition that calls a large language model.
	NodeKindLLM NodeKind = "llm"

	// NodeKindValidate represents a transition that validates tokens against a JSON schema.
	NodeKindValidate NodeKind = "validate"

	// NodeKindSubNet represents a transition that delegates to a child CPN.
	NodeKindSubNet NodeKind = "subnet"

	// NodeKindObserver represents a transition that monitors events from sub-CPNs.
	NodeKindObserver NodeKind = "observer"

	// NodeKindHITL represents a transition that blocks until human input arrives.
	NodeKindHITL NodeKind = "hitl"
)
