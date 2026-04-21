package app

import (
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
}
