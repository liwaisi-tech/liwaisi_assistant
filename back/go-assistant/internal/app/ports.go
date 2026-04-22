package app

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// SessionToolRegistry is the tool-registry port used by SessionService.
// The concrete implementation is *cpn/tools.Registry; this interface keeps
// the app layer free of that concrete type so tests can substitute a stub.
//
// Method set is intentionally narrow — only the three operations SessionService
// actually calls. Admin operations live in the httpapi driving adapter's port.
type SessionToolRegistry interface {
	// cpn.ToolRegistry — inherited so the CPN executor can call
	// RegisterManifest via NodeKindRegisterTool without an extra cast.
	cpn.ToolRegistry

	// InjectIntoCPN stamps ToolMeta + Executor onto matching NodeKindTool
	// transitions. Called once per session at topology creation time.
	InjectIntoCPN(c *cpn.CPN)

	// Resolve returns the live ToolEntry for a qualified name or false when
	// the name is not registered.
	Resolve(qualifiedName string) (*tools.ToolEntry, bool)

	// ListUserAuthored returns non-deprecated entries registered at runtime
	// by the operator through system/register_tool. Session bootstrap
	// materialises one NodeKindTool transition per entry so the LLM sees
	// them as first-class callable tools in every new session (see
	// spec/spec-architecture-user-tool-session-surface.md).
	ListUserAuthored() []*tools.ToolEntry
}

// ToolboxLister is the aggregate read-only view over registered tools
// grouped by toolbox, consumed by the personality-catalogue injector on
// turn ≥ 2 (spec-architecture-brae-awakening-toolbox-extension.md
// REQ-006/REQ-007). Mirrors the driving-side port of the same name;
// duplicated here so the app layer has a local interface and the
// driving adapter does not import app. The concrete implementation is
// *cpn/tools.Registry.
type ToolboxLister interface {
	Toolboxes(ctx context.Context) []tools.ToolboxManifest
}
