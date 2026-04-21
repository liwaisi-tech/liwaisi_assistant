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

	// NodeKindBash represents a transition that executes a shell command on
	// the host OS via the HostAdapter port (GAP-1). The executor dispatches
	// these to fire_bash.go.
	NodeKindBash NodeKind = "bash"

	// NodeKindRegisterTool represents a transition that publishes a new
	// tool to the runtime-mutable ToolRegistry (GAP-3). Consumes a
	// ColorToolManifest token and deposits a ColorArtifact token carrying
	// the qualified name + UUID of the newly registered entry.
	NodeKindRegisterTool NodeKind = "register_tool"

	// NodeKindSynthesize represents an LLM-backed transition that emits
	// a new CPN topology (GAP-4). The transition interpolates the
	// consumed token's payload into a prompt, asks the LLM for a
	// topology that uses only SafeRegistry primitives, lints it, persists
	// the result in FlowRepository, and deposits a ColorFlowRef token.
	NodeKindSynthesize NodeKind = "synthesize"

	// NodeKindInstantiate represents a transition that consumes a
	// ColorFlowRef token (GAP-4), loads the stored topology from
	// FlowRepository, re-validates it, and spawns it as a sub-CPN.
	// First instantiation per session goes through HITL; subsequent ones
	// short-circuit via the session's approved-flows set.
	NodeKindInstantiate NodeKind = "instantiate"

	// NodeKindTopologyMutate represents a transition that applies a hot
	// topology mutation to the running CPN (GAP-7). The consumed token
	// must carry a JSON-serialised Mutation as its payload.
	NodeKindTopologyMutate NodeKind = "topology_mutate"
)
