package grpc_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	agentv1 "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/api/gen/agent/v1"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	mygrpc "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/grpc"
)

type mockAgentService struct {
	chatStreamFunc func(ctx context.Context, sessionID string, inCh <-chan valueobject.ClientMessage, outCh chan<- valueobject.ServerMessage)
}

func (m *mockAgentService) Chat(ctx context.Context, sessionID string, userMessage string) (<-chan valueobject.StreamChunk, error) {
	return nil, nil
}

func (m *mockAgentService) Ask(ctx context.Context, query string) (string, error) {
	return "", nil
}

func (m *mockAgentService) ChatStream(ctx context.Context, sessionID string, inCh <-chan valueobject.ClientMessage, outCh chan<- valueobject.ServerMessage) {
	if m.chatStreamFunc != nil {
		m.chatStreamFunc(ctx, sessionID, inCh, outCh)
	}
}

func setupBufconnServer(t *testing.T, srv *mygrpc.AgentServer) (agentv1.AgentServiceClient, func()) {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(s, srv)

	go func() {
		if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("Server exited with error: %v", err)
		}
	}()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	cleanup := func() {
		conn.Close()
		s.Stop()
	}

	return agentv1.NewAgentServiceClient(conn), cleanup
}

func TestAgentServer_ChatStream(t *testing.T) {
	mockSvc := &mockAgentService{
		chatStreamFunc: func(ctx context.Context, sessionID string, inCh <-chan valueobject.ClientMessage, outCh chan<- valueobject.ServerMessage) {
			defer close(outCh)
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-inCh:
					if !ok {
						return
					}
					if msg.Text != "" {
						outCh <- valueobject.ServerMessage{Chunk: "echo: " + msg.Text}
					}
				}
			}
		},
	}

	agentServer := mygrpc.NewAgentServer(mockSvc)
	client, cleanup := setupBufconnServer(t, agentServer)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.ChatStream(ctx)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	err = stream.Send(&agentv1.ClientMessage{
		Payload: &agentv1.ClientMessage_Text{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}

	chunk, ok := resp.Payload.(*agentv1.ServerMessage_Chunk)
	if !ok || chunk.Chunk != "echo: hello" {
		t.Fatalf("Expected chunk 'echo: hello', got %v", resp.Payload)
	}

	_ = stream.CloseSend()
	_, err = stream.Recv()
	if err != io.EOF {
		t.Fatalf("Expected EOF, got %v", err)
	}
}
