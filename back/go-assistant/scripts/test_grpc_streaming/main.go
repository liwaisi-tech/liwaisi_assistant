package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	agentv1 "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/api/gen/agent/v1"
)

func main() {
	// Connect to the local server over h2c (insecure HTTP/2)
	conn, err := grpc.NewClient("localhost:8080",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := agentv1.NewAgentServiceClient(conn)

	// Create context with metadata for authentication
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Add the bearer token and a custom session ID
	md := metadata.New(map[string]string{
		"authorization": "Bearer dummy-test-token",
		"session-id":    "test-session-123",
	})
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Initiate ChatStream
	stream, err := client.ChatStream(ctx)
	if err != nil {
		log.Fatalf("could not start ChatStream: %v", err)
	}

	// Send a message
	fmt.Printf(">> Sending message to server...\n")
	err = stream.Send(&agentv1.ClientMessage{
		Payload: &agentv1.ClientMessage_Text{
			Text: "Introduce your self in colombian spanish.",
		},
	})
	if err != nil {
		log.Fatalf("failed to send message: %v", err)
	}

	// Receive the streaming response
	fmt.Printf("<< Receiving responses:\n")
	for {
		resp, err := stream.Recv()
		if err != nil {
			log.Printf("stream ended: %v", err)
			break
		}

		switch p := resp.Payload.(type) {
		case *agentv1.ServerMessage_Chunk:
			fmt.Printf("%s", p.Chunk)
		case *agentv1.ServerMessage_Error:
			fmt.Printf("\n[ERROR from server: %s]\n", p.Error)
		case *agentv1.ServerMessage_ToolCall:
			fmt.Printf("\n[TOOL REQUEST: %s (%s)]\n", p.ToolCall.Name, p.ToolCall.Arguments)
		}
	}
	fmt.Println()
}
