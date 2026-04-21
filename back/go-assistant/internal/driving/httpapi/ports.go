package httpapi

// ports.go — driving-adapter port interfaces for the HTTP layer.
//
// Each interface is the minimal surface the HTTP handlers need from the
// application and domain layers. Concrete types satisfy these interfaces
// implicitly; no code-gen or registration is required.
//
// Hexagonal rule: driving adapters (this package) depend on ports, never on
// concrete application types. The composition root (cmd/server/main.go) wires
// the concrete implementations at start-up.

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// SessionPort is the application-service boundary used by HTTP session handlers.
// The concrete implementation is *app.SessionService.
type SessionPort interface {
	CreateSession(ctx context.Context, userID string, channel cpn.ChannelType) (*app.SessionInfo, error)
	GetSession(sessionID string) (*app.SessionInfo, error)
	UpdateSession(ctx context.Context, sessionID string, title *string, softDelete bool) error
	DeleteSession(sessionID string) error
	ListSessions(ctx context.Context, userID string, opts *persist.SessionListOpts) (*persist.Page[*persist.SessionListItem], error)
	ForkSession(ctx context.Context, sourceSessionID, userID string, channel cpn.ChannelType, messageIndex int) (*app.SessionInfo, error)
	SendMessage(ctx context.Context, sessionID, content string) error
	StreamChannel(sessionID string) (<-chan cpn.StreamChunk, error)
	SendStreamChunk(sessionID string, chunk cpn.StreamChunk) error
	CloseStream(sessionID string) error
	ResolveHITL(ctx context.Context, sessionID, transitionID string, resp cpn.HITLResponse) error
	SetEventCallback(fn func(string, cpn.Event))
}

// ToolRegistryPort is the tool-registry boundary used by HTTP tool handlers.
// The concrete implementation is *cpn/tools.Registry.
type ToolRegistryPort interface {
	List(namespace string) []*tools.ToolSchema
	ListAll() []*tools.ToolSchema
	Resolve(qualifiedName string) (*tools.ToolEntry, bool)
	Get(ctx context.Context, qualifiedName string) (*tools.ToolEntry, error)
	Deprecate(ctx context.Context, qualifiedName, reason string) error
	Unregister(ctx context.Context, qualifiedName string) error
	ListFiltered(ctx context.Context, f tools.ToolFilter) []*tools.ToolEntry
}

// SkillManifestPort is the skill-manifest boundary used by the admin skills
// HTTP handlers (GAP-8). The concrete implementation is *app.SkillManifestService.
type SkillManifestPort interface {
	Build(ctx context.Context) (app.Manifest, error)
	BuildCompact(ctx context.Context, sizeCap int) (string, error)
	Invalidate(reason string)
}
