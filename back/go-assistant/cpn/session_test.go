package cpn

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// testSessionWithHITL creates a Session with a root CPN containing a HITL transition.
func testSessionWithHITL() *Session {
	transitions := map[string]*Transition{
		"T:HITL": {ID: "T:HITL", Kind: NodeKindHITL},
		"T:TOOL": {ID: "T:TOOL", Kind: NodeKindTool},
	}
	root := &CPN{ID: "root", Transitions: transitions}
	return NewSession("sess-1", "user-1", ChannelWeb, root)
}

func TestNewSession(t *testing.T) {
	root := &CPN{ID: "root"}
	before := time.Now()
	sess := NewSession("s1", "u1", ChannelWhatsApp, root)
	after := time.Now()

	if sess.ID != "s1" {
		t.Fatalf("ID = %q, want %q", sess.ID, "s1")
	}
	if sess.UserID != "u1" {
		t.Fatalf("UserID = %q, want %q", sess.UserID, "u1")
	}
	if sess.Channel != ChannelWhatsApp {
		t.Fatalf("Channel = %q, want %q", sess.Channel, ChannelWhatsApp)
	}
	if sess.Root != root {
		t.Fatal("Root should point to the provided CPN")
	}
	if cap(sess.Stream) != DefaultStreamBuffer {
		t.Fatalf("Stream cap = %d, want %d", cap(sess.Stream), DefaultStreamBuffer)
	}
	if len(sess.HITLInject) != 0 {
		t.Fatalf("HITLInject should be empty, got %d entries", len(sess.HITLInject))
	}
	if sess.Messages() != nil {
		t.Fatal("Messages() should return nil for empty history")
	}
	if sess.CreatedAt.Before(before) || sess.CreatedAt.After(after) {
		t.Fatalf("CreatedAt %v not in [%v, %v]", sess.CreatedAt, before, after)
	}
}

func TestSession_RegisterHITL_Valid(t *testing.T) {
	sess := testSessionWithHITL()
	ch := make(chan Token, 1)

	if err := sess.RegisterHITL("T:HITL", ch); err != nil {
		t.Fatalf("RegisterHITL valid: %v", err)
	}

	sess.mu.RLock()
	got, ok := sess.HITLInject["T:HITL"]
	sess.mu.RUnlock()
	if !ok || got != ch {
		t.Fatal("channel should be stored in HITLInject")
	}
}

func TestSession_RegisterHITL_NonExistent(t *testing.T) {
	sess := testSessionWithHITL()
	ch := make(chan Token, 1)

	err := sess.RegisterHITL("T:NOPE", ch)
	if err == nil {
		t.Fatal("expected error for non-existent transition")
	}
}

func TestSession_RegisterHITL_NonHITLKind(t *testing.T) {
	sess := testSessionWithHITL()
	ch := make(chan Token, 1)

	err := sess.RegisterHITL("T:TOOL", ch)
	if err == nil {
		t.Fatal("expected error for non-HITL kind transition")
	}
}

func TestSession_RegisterHITL_Duplicate(t *testing.T) {
	sess := testSessionWithHITL()
	ch := make(chan Token, 1)

	if err := sess.RegisterHITL("T:HITL", ch); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := sess.RegisterHITL("T:HITL", ch)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
	if !errors.Is(err, ErrHITLAlreadyRegistered) {
		t.Fatalf("expected ErrHITLAlreadyRegistered, got %v", err)
	}
}

func TestSession_RegisterHITL_NilRoot(t *testing.T) {
	sess := NewSession("s1", "u1", ChannelWeb, nil)
	ch := make(chan Token, 1)

	err := sess.RegisterHITL("T:HITL", ch)
	if err == nil {
		t.Fatal("expected error for nil root")
	}
}

func TestSession_UnregisterHITL(t *testing.T) {
	sess := testSessionWithHITL()
	ch := make(chan Token, 1)

	if err := sess.RegisterHITL("T:HITL", ch); err != nil {
		t.Fatalf("register: %v", err)
	}
	sess.UnregisterHITL("T:HITL")

	err := sess.ResolveHITL("T:HITL", HITLResponse{Action: HITLApprove})
	if !errors.Is(err, ErrNoHITLWaiting) {
		t.Fatalf("expected ErrNoHITLWaiting after unregister, got %v", err)
	}
}

func TestSession_UnregisterHITL_UnknownID(t *testing.T) {
	sess := testSessionWithHITL()
	// Should not panic.
	sess.UnregisterHITL("T:UNKNOWN")
}

func TestSession_ResolveHITL_Success(t *testing.T) {
	sess := testSessionWithHITL()
	ch := make(chan Token, 1)

	if err := sess.RegisterHITL("T:HITL", ch); err != nil {
		t.Fatalf("register: %v", err)
	}

	resp := HITLResponse{Action: HITLApprove, Content: "looks good"}
	if err := sess.ResolveHITL("T:HITL", resp); err != nil {
		t.Fatalf("ResolveHITL: %v", err)
	}

	select {
	case tok := <-ch:
		if tok.Color != ColorHuman {
			t.Errorf("Color = %q, want %q", tok.Color, ColorHuman)
		}
		if tok.OriginID != "human" {
			t.Errorf("OriginID = %q, want %q", tok.OriginID, "human")
		}
		if tok.OriginDepth != -1 {
			t.Errorf("OriginDepth = %d, want -1", tok.OriginDepth)
		}
		if tok.OriginKind != NodeKindHITL {
			t.Errorf("OriginKind = %q, want %q", tok.OriginKind, NodeKindHITL)
		}
		if tok.Space != SpaceSurface {
			t.Errorf("Space = %q, want %q", tok.Space, SpaceSurface)
		}
		if tok.SessionID != "sess-1" {
			t.Errorf("SessionID = %q, want %q", tok.SessionID, "sess-1")
		}
		payload, ok := tok.Payload.(HITLResponse)
		if !ok {
			t.Fatalf("Payload type = %T, want HITLResponse", tok.Payload)
		}
		if payload.Action != HITLApprove || payload.Content != "looks good" {
			t.Errorf("Payload = %+v, want {Approve, looks good}", payload)
		}
	default:
		t.Fatal("expected token on channel")
	}
}

func TestSession_ResolveHITL_NoWaiting(t *testing.T) {
	sess := testSessionWithHITL()

	err := sess.ResolveHITL("T:HITL", HITLResponse{Action: HITLApprove})
	if !errors.Is(err, ErrNoHITLWaiting) {
		t.Fatalf("expected ErrNoHITLWaiting, got %v", err)
	}
}

func TestSession_ResolveHITL_TokenMetadata(t *testing.T) {
	sess := testSessionWithHITL()
	ch := make(chan Token, 1)

	if err := sess.RegisterHITL("T:HITL", ch); err != nil {
		t.Fatalf("register: %v", err)
	}

	before := time.Now()
	if err := sess.ResolveHITL("T:HITL", HITLResponse{Action: HITLRevise, Content: "fix this"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	after := time.Now()

	tok := <-ch
	if tok.Timestamp.Before(before) || tok.Timestamp.After(after) {
		t.Errorf("Timestamp %v not in [%v, %v]", tok.Timestamp, before, after)
	}
}

func TestSession_StreamChunk_Routing(t *testing.T) {
	sess := testSessionWithHITL()

	chunk := StreamChunk{
		SessionID: "sess-1",
		CPNID:     "root",
		CPNRole:   "coordinator",
		Content:   "hello",
		Done:      false,
	}

	sess.Stream <- chunk

	select {
	case got := <-sess.Stream:
		if got.SessionID != chunk.SessionID {
			t.Errorf("SessionID = %q, want %q", got.SessionID, chunk.SessionID)
		}
		if got.Content != "hello" {
			t.Errorf("Content = %q, want %q", got.Content, "hello")
		}
		if got.Done != false {
			t.Error("Done should be false")
		}
	default:
		t.Fatal("expected chunk on stream")
	}
}

func TestSession_AppendMessage(t *testing.T) {
	sess := testSessionWithHITL()

	msgs := []Message{
		{ID: "m1", Role: RoleUser, Content: "hello"},
		{ID: "m2", Role: RoleAssistant, Content: "hi there"},
		{ID: "m3", Role: RoleUser, Content: "thanks"},
	}
	for i := range msgs {
		sess.AppendMessage(&msgs[i])
	}

	got := sess.Messages()
	if len(got) != 3 {
		t.Fatalf("Messages() len = %d, want 3", len(got))
	}
	for i, m := range got {
		if m.ID != msgs[i].ID {
			t.Errorf("Messages()[%d].ID = %q, want %q", i, m.ID, msgs[i].ID)
		}
		if m.Content != msgs[i].Content {
			t.Errorf("Messages()[%d].Content = %q, want %q", i, m.Content, msgs[i].Content)
		}
	}
}

func TestSession_Messages_ReturnsCopy(t *testing.T) {
	sess := testSessionWithHITL()
	sess.AppendMessage(&Message{ID: "m1", Role: RoleUser, Content: "original"})

	got := sess.Messages()
	got[0].Content = "mutated"

	internal := sess.Messages()
	if internal[0].Content != "original" {
		t.Fatalf("internal Content = %q, want %q — Messages() did not return a copy", internal[0].Content, "original")
	}
}

func TestSession_Messages_EmptyReturnsNil(t *testing.T) {
	sess := testSessionWithHITL()
	if got := sess.Messages(); got != nil {
		t.Fatalf("Messages() = %v, want nil for empty history", got)
	}
}

func TestSession_Concurrent_HITLRegisterResolve(t *testing.T) {
	transitions := make(map[string]*Transition)
	for i := range 100 {
		id := "T:HITL-" + itoa(i)
		transitions[id] = &Transition{ID: id, Kind: NodeKindHITL}
	}
	root := &CPN{ID: "root", Transitions: transitions}
	sess := NewSession("sess-conc", "u1", ChannelWeb, root)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := "T:HITL-" + itoa(idx)
			ch := make(chan Token, 1)
			if err := sess.RegisterHITL(id, ch); err != nil {
				t.Errorf("register %s: %v", id, err)
				return
			}
			if err := sess.ResolveHITL(id, HITLResponse{Action: HITLApprove}); err != nil {
				t.Errorf("resolve %s: %v", id, err)
				return
			}
			<-ch
			sess.UnregisterHITL(id)
		}(i)
	}
	wg.Wait()
}

func TestSession_Concurrent_AppendMessages(t *testing.T) {
	sess := testSessionWithHITL()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sess.AppendMessage(&Message{
				ID:      "m-" + itoa(idx),
				Role:    RoleUser,
				Content: "msg " + itoa(idx),
			})
			_ = sess.Messages()
		}(i)
	}
	wg.Wait()

	got := sess.Messages()
	if len(got) != 100 {
		t.Fatalf("Messages() len = %d, want 100", len(got))
	}
}

// itoa is a minimal int-to-string helper to avoid importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
