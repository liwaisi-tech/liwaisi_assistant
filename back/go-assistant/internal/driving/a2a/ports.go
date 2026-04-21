package a2a

// ports.go — driving-adapter port interfaces for the A2A layer.
//
// SessionPort is the minimal surface BRAEExecutor needs from the application
// service. The concrete implementation is *app.SessionService.

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// SessionPort is the application-service boundary used by BRAEExecutor.
type SessionPort interface {
	CreateSession(ctx context.Context, userID string, channel cpn.ChannelType) (*app.SessionInfo, error)
	GetSession(sessionID string) (*app.SessionInfo, error)
	DeleteSession(sessionID string) error
	ListSessions(ctx context.Context, userID string, opts *persist.SessionListOpts) (*persist.Page[*persist.SessionListItem], error)
	SendMessage(ctx context.Context, sessionID, content string) error
	StreamChannel(sessionID string) (<-chan cpn.StreamChunk, error)
}
