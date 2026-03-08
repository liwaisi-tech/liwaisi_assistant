package grpc

import (
	"errors"
	"io"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	agentv1 "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/api/gen/agent/v1"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

type AgentServer struct {
	agentSvc input.AgentService
	agentv1.UnimplementedAgentServiceServer
}

func NewAgentServer(agentSvc input.AgentService) *AgentServer {
	return &AgentServer{agentSvc: agentSvc}
}

func (s *AgentServer) ChatStream(stream agentv1.AgentService_ChatStreamServer) error {
	ctx := stream.Context()

	// Extract session ID from GRPC metadata.
	sessionID := "default-web-session"
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("session-id"); len(vals) > 0 && vals[0] != "" {
			sessionID = vals[0]
		}
	}

	slog.InfoContext(ctx, "GRPC ChatStream connected", "session_id", sessionID)

	inCh := make(chan valueobject.ClientMessage, 10)
	outCh := make(chan valueobject.ServerMessage, 10)

	// Start the domain streaming service in the background.
	go s.agentSvc.ChatStream(ctx, sessionID, inCh, outCh)

	errCh := make(chan error, 1)

	// Goroutine for handling outgoing Server messages.
	go func() {
		for srvMsg := range outCh {
			var reply agentv1.ServerMessage
			switch {
			case srvMsg.Chunk != "":
				reply.Payload = &agentv1.ServerMessage_Chunk{Chunk: srvMsg.Chunk}
			case srvMsg.Error != nil:
				reply.Payload = &agentv1.ServerMessage_Error{Error: srvMsg.Error.Error()}
			case srvMsg.ToolCall != nil:
				reply.Payload = &agentv1.ServerMessage_ToolCall{
					ToolCall: &agentv1.ToolCall{
						Id:        srvMsg.ToolCall.ID,
						Name:      srvMsg.ToolCall.Function.Name,
						Arguments: srvMsg.ToolCall.Function.Arguments,
					},
				}
			}

			if err := stream.Send(&reply); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil // Normal graceful exit when outCh is closed.
	}()

	// Loop for reading incoming Client messages.
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			slog.InfoContext(ctx, "GRPC ChatStream EOF received", "session_id", sessionID)
			close(inCh)
			break
		}
		if err != nil {
			slog.ErrorContext(ctx, "GRPC ChatStream Recv error", "error", err)
			close(inCh)
			return status.Errorf(codes.Canceled, "stream receive error: %v", err)
		}

		var cliMsg valueobject.ClientMessage
		switch p := req.Payload.(type) {
		case *agentv1.ClientMessage_Text:
			cliMsg.Text = p.Text
		case *agentv1.ClientMessage_ToolResult:
			cliMsg.ToolResult = &valueobject.ToolResult{
				ID:     p.ToolResult.Id,
				Result: p.ToolResult.Result,
			}
		}

		select {
		case <-ctx.Done():
			slog.WarnContext(ctx, "GRPC ChatStream context done while receiving", "session_id", sessionID)
			close(inCh)
			return ctx.Err()
		case inCh <- cliMsg:
		}
	}

	// Wait for the send goroutine to finish or the connection context to be canceled.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}
