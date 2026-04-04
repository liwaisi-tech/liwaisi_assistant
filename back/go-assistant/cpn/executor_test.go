package cpn

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Test Helpers ---

// echoTool returns a tool executor that transforms the payload.
func echoTool(transform func(any) any) func(context.Context, Token) (Token, error) {
	return func(_ context.Context, in Token) (Token, error) {
		return Token{
			Color:   in.Color,
			Payload: transform(in.Payload),
			Space:   in.Space,
		}, nil
	}
}

// failingTool returns a tool executor that fails failCount times then succeeds.
func failingTool(failCount int) func(context.Context, Token) (Token, error) {
	var calls atomic.Int32
	return func(_ context.Context, in Token) (Token, error) {
		if int(calls.Add(1)) <= failCount {
			return Token{}, errors.New("transient error")
		}
		return Token{Color: in.Color, Payload: "success", Space: in.Space}, nil
	}
}

// alwaysFailTool returns a tool executor that always fails.
func alwaysFailTool() func(context.Context, Token) (Token, error) {
	return func(_ context.Context, _ Token) (Token, error) {
		return Token{}, errors.New("permanent error")
	}
}

// identityTool returns the input token unchanged.
func identityTool() func(context.Context, Token) (Token, error) {
	return func(_ context.Context, in Token) (Token, error) {
		return Token{Color: in.Color, Payload: in.Payload, Space: in.Space}, nil
	}
}

// buildSimpleCPN creates: P:IN -> [T:TOOL] -> P:OUT with a single token in P:IN.
func buildSimpleCPN(executor func(context.Context, Token) (Token, error)) *CPN {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "hello"})

	t := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	t.Executor = executor

	transitions := map[string]*Transition{"T:TOOL": t}
	return NewCPN("cpn-test", "worker", 2, ModeMAS, "sess-1", places, transitions)
}

// --- Run Tests ---

func TestCPN_Run_SingleToolTransition(t *testing.T) {
	c := buildSimpleCPN(echoTool(func(p any) any {
		return strings.ToUpper(p.(string))
	}))

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("State = %q, want %q", c.State, StateCompleted)
	}

	out := c.Places["P:OUT"]
	if out.Len() != 1 {
		t.Fatalf("P:OUT.Len() = %d, want 1", out.Len())
	}
	tokens, _ := out.Peek()
	if tokens[0].Payload != "HELLO" {
		t.Fatalf("Payload = %v, want %q", tokens[0].Payload, "HELLO")
	}
}

func TestCPN_Run_LinearChain(t *testing.T) {
	// P:IN -> [T:A] -> P:MID -> [T:B] -> P:OUT
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:MID": NewPlace("P:MID", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "a"})

	tA := NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:MID"})
	tA.Executor = echoTool(func(p any) any { return p.(string) + "->A" })

	tB := NewTransition("T:B", NodeKindTool, []string{"P:MID"}, []string{"P:OUT"})
	tB.Executor = echoTool(func(p any) any { return p.(string) + "->B" })

	transitions := map[string]*Transition{"T:A": tA, "T:B": tB}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("State = %q, want %q", c.State, StateCompleted)
	}

	tokens, _ := places["P:OUT"].Peek()
	if tokens[0].Payload != "a->A->B" {
		t.Fatalf("Payload = %v, want %q", tokens[0].Payload, "a->A->B")
	}
}

func TestCPN_Run_ParallelTransitions(t *testing.T) {
	// P:IN1 -> [T:LEFT]  -> P:OUT1
	// P:IN2 -> [T:RIGHT] -> P:OUT2
	// Two independent transitions fire concurrently.
	places := map[string]*Place{
		"P:IN1":  NewPlace("P:IN1", ColorString, SpaceSurface),
		"P:IN2":  NewPlace("P:IN2", ColorString, SpaceSurface),
		"P:OUT1": NewPlace("P:OUT1", ColorString, SpaceSurface),
		"P:OUT2": NewPlace("P:OUT2", ColorString, SpaceSurface),
	}
	_ = places["P:IN1"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "left"})
	_ = places["P:IN2"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "right"})

	var started atomic.Int32

	tL := NewTransition("T:LEFT", NodeKindTool, []string{"P:IN1"}, []string{"P:OUT1"})
	tL.Executor = func(_ context.Context, in Token) (Token, error) {
		started.Add(1)
		// Small sleep to allow overlap detection.
		time.Sleep(10 * time.Millisecond)
		return Token{Color: in.Color, Payload: "L", Space: in.Space}, nil
	}

	tR := NewTransition("T:RIGHT", NodeKindTool, []string{"P:IN2"}, []string{"P:OUT2"})
	tR.Executor = func(_ context.Context, in Token) (Token, error) {
		started.Add(1)
		time.Sleep(10 * time.Millisecond)
		return Token{Color: in.Color, Payload: "R", Space: in.Space}, nil
	}

	transitions := map[string]*Transition{"T:LEFT": tL, "T:RIGHT": tR}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("State = %q, want %q", c.State, StateCompleted)
	}

	// Both fired (2 goroutines started).
	if started.Load() != 2 {
		t.Fatalf("started = %d, want 2", started.Load())
	}

	if places["P:OUT1"].Len() != 1 {
		t.Fatalf("P:OUT1.Len() = %d, want 1", places["P:OUT1"].Len())
	}
	if places["P:OUT2"].Len() != 1 {
		t.Fatalf("P:OUT2.Len() = %d, want 1", places["P:OUT2"].Len())
	}
}

func TestCPN_Run_DiamondTopology(t *testing.T) {
	// P:INPUT -> [T:FORK] -> P:A, P:B
	// P:A -> [T:LEFT]  -> P:LEFT_OUT
	// P:B -> [T:RIGHT] -> P:RIGHT_OUT
	// P:LEFT_OUT, P:RIGHT_OUT -> [T:JOIN] -> P:OUTPUT
	places := map[string]*Place{
		"P:INPUT":     NewPlace("P:INPUT", ColorString, SpaceSurface),
		"P:A":         NewPlace("P:A", ColorString, SpaceSurface),
		"P:B":         NewPlace("P:B", ColorString, SpaceSurface),
		"P:LEFT_OUT":  NewPlace("P:LEFT_OUT", ColorString, SpaceSurface),
		"P:RIGHT_OUT": NewPlace("P:RIGHT_OUT", ColorString, SpaceSurface),
		"P:OUTPUT":    NewPlace("P:OUTPUT", ColorString, SpaceSurface),
	}
	_ = places["P:INPUT"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "start"})

	tFork := NewTransition("T:FORK", NodeKindTool, []string{"P:INPUT"}, []string{"P:A", "P:B"})
	tFork.Executor = identityTool()

	tLeft := NewTransition("T:LEFT", NodeKindTool, []string{"P:A"}, []string{"P:LEFT_OUT"})
	tLeft.Executor = echoTool(func(p any) any { return p.(string) + "-left" })

	tRight := NewTransition("T:RIGHT", NodeKindTool, []string{"P:B"}, []string{"P:RIGHT_OUT"})
	tRight.Executor = echoTool(func(p any) any { return p.(string) + "-right" })

	tJoin := NewTransition("T:JOIN", NodeKindTool, []string{"P:LEFT_OUT", "P:RIGHT_OUT"}, []string{"P:OUTPUT"})
	tJoin.Executor = echoTool(func(p any) any { return "joined" })

	transitions := map[string]*Transition{
		"T:FORK": tFork, "T:LEFT": tLeft, "T:RIGHT": tRight, "T:JOIN": tJoin,
	}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("State = %q, want %q", c.State, StateCompleted)
	}

	tokens, _ := places["P:OUTPUT"].Peek()
	if tokens[0].Payload != "joined" {
		t.Fatalf("Payload = %v, want %q", tokens[0].Payload, "joined")
	}
}

func TestCPN_Run_Deadlock(t *testing.T) {
	// P:IN -> [T:A] -> P:OUT, but P:IN is empty → deadlock.
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	tr := NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = identityTool()

	transitions := map[string]*Transition{"T:A": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if !errors.Is(err, ErrDeadlock) {
		t.Fatalf("Run() error = %v, want ErrDeadlock", err)
	}
	if c.State != StateFailed {
		t.Fatalf("State = %q, want %q", c.State, StateFailed)
	}
}

func TestCPN_Run_Timeout(t *testing.T) {
	// Transition sleeps forever — context deadline should catch it.
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	// Executor that deposits token back to P:IN to keep the loop spinning.
	tr.Executor = func(_ context.Context, in Token) (Token, error) {
		return Token{Color: in.Color, Payload: in.Payload, Space: in.Space}, nil
	}
	// Make it circular so the CPN never completes: output goes back to input.
	tr.OutputPlaces = []string{"P:IN"}

	transitions := map[string]*Transition{"T:A": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.Run(ctx)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Run() error = %v, want ErrTimeout", err)
	}
	if c.State != StateFailed {
		t.Fatalf("State = %q, want %q", c.State, StateFailed)
	}
}

func TestCPN_Run_ValidationFails(t *testing.T) {
	// Transition references non-existent place.
	places := map[string]*Place{
		"P:IN": NewPlace("P:IN", ColorString, SpaceSurface),
	}
	tr := NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:MISSING"})
	tr.Executor = identityTool()

	transitions := map[string]*Transition{"T:A": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want validation error")
	}
	if c.State != StateFailed {
		t.Fatalf("State = %q, want %q", c.State, StateFailed)
	}
}

func TestCPN_Run_ExecutorError_NoErrorPlace(t *testing.T) {
	c := buildSimpleCPN(alwaysFailTool())

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if c.State != StateFailed {
		t.Fatalf("State = %q, want %q", c.State, StateFailed)
	}
}

func TestCPN_Run_ExecutorError_WithErrorPlace(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
		"P:ERR": NewPlace("P:ERR", ColorError, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = alwaysFailTool()
	tr.ErrorPlace = "P:ERR"

	transitions := map[string]*Transition{"T:TOOL": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	// CPN should complete because P:OUT and P:ERR are both terminals;
	// P:ERR got a token, but P:OUT is also terminal and empty.
	// Actually: terminal places = P:OUT (not input to anything) and P:ERR (not input).
	// P:OUT is empty, so IsComplete = false → deadlock after error routing.
	// This is correct behavior: error was routed, but CPN can't complete.
	if !errors.Is(err, ErrDeadlock) {
		t.Fatalf("Run() error = %v, want ErrDeadlock (P:OUT still empty)", err)
	}

	// But P:ERR should have the error token.
	if places["P:ERR"].Len() != 1 {
		t.Fatalf("P:ERR.Len() = %d, want 1", places["P:ERR"].Len())
	}
	tokens, _ := places["P:ERR"].Peek()
	if tokens[0].Color != ColorError {
		t.Fatalf("error token Color = %q, want %q", tokens[0].Color, ColorError)
	}
}

func TestCPN_Run_ExecutorError_WithErrorPlace_Completes(t *testing.T) {
	// Topology where ErrorPlace IS the terminal: P:IN -> [T:TOOL] -> P:OUT
	// ErrorPlace = P:OUT. If tool fails, error token goes to P:OUT, CPN completes.
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorError, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorError, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorError, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = alwaysFailTool()
	tr.ErrorPlace = "P:OUT"

	transitions := map[string]*Transition{"T:TOOL": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v, want nil (error routed to terminal)", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("State = %q, want %q", c.State, StateCompleted)
	}
}

func TestCPN_Run_RetrySuccess(t *testing.T) {
	// Fail once, succeed on second attempt.
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = failingTool(1)
	tr.Retry = &RetryPolicy{
		MaxAttempts: 3,
		InitialWait: time.Millisecond,
		MaxWait:     10 * time.Millisecond,
		Multiplier:  1.0,
	}

	transitions := map[string]*Transition{"T:TOOL": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("State = %q, want %q", c.State, StateCompleted)
	}
}

func TestCPN_Run_RetryExhausted_ErrorPlace(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorError, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorError, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorError, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = alwaysFailTool()
	tr.ErrorPlace = "P:OUT"
	tr.Retry = &RetryPolicy{
		MaxAttempts: 2,
		InitialWait: time.Millisecond,
		MaxWait:     10 * time.Millisecond,
		Multiplier:  1.0,
	}

	transitions := map[string]*Transition{"T:TOOL": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v, want nil (error routed to terminal)", err)
	}

	if places["P:OUT"].Len() != 1 {
		t.Fatalf("P:OUT.Len() = %d, want 1", places["P:OUT"].Len())
	}
	tokens, _ := places["P:OUT"].Peek()
	if tokens[0].Color != ColorError {
		t.Fatalf("token Color = %q, want %q", tokens[0].Color, ColorError)
	}
}

func TestCPN_Run_RetryExhausted_NoErrorPlace(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = alwaysFailTool()
	tr.Retry = &RetryPolicy{
		MaxAttempts: 2,
		InitialWait: time.Millisecond,
		MaxWait:     10 * time.Millisecond,
		Multiplier:  1.0,
	}

	transitions := map[string]*Transition{"T:TOOL": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if c.State != StateFailed {
		t.Fatalf("State = %q, want %q", c.State, StateFailed)
	}
}

func TestCPN_Run_CircuitBreakerOpen(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	cfg := &CircuitBreakerConfig{FailureThreshold: 1, OpenDuration: 10 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }
	cb.RecordFailure() // trips the breaker

	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = identityTool()
	tr.SetCircuitBreaker(cb)

	transitions := map[string]*Transition{"T:TOOL": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	// CB open -> CanFire returns false -> no firable -> deadlock.
	if !errors.Is(err, ErrDeadlock) {
		t.Fatalf("Run() error = %v, want ErrDeadlock", err)
	}
}

func TestCPN_Run_MultipleOutputPlaces(t *testing.T) {
	places := map[string]*Place{
		"P:IN":   NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT1": NewPlace("P:OUT1", ColorString, SpaceSurface),
		"P:OUT2": NewPlace("P:OUT2", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT1", "P:OUT2"})
	tr.Executor = identityTool()

	transitions := map[string]*Transition{"T:TOOL": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if places["P:OUT1"].Len() != 1 {
		t.Fatalf("P:OUT1.Len() = %d, want 1", places["P:OUT1"].Len())
	}
	if places["P:OUT2"].Len() != 1 {
		t.Fatalf("P:OUT2.Len() = %d, want 1", places["P:OUT2"].Len())
	}
}

func TestCPN_Run_ContextCancel(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:IN"})
	tr.Executor = identityTool()

	transitions := map[string]*Transition{"T:TOOL": tr}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately after a short delay.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := c.Run(ctx)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Run() error = %v, want ErrTimeout", err)
	}
}

func TestCPN_Run_NoGoroutineLeaks(t *testing.T) {
	// Stabilize goroutine count.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	c := buildSimpleCPN(identityTool())

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Allow goroutines to settle.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Allow small variance (GC, test runtime).
	if after > before+2 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestCPN_Run_OutputTokensStamped(t *testing.T) {
	c := buildSimpleCPN(identityTool())
	c.ID = "cpn-stamp"
	c.Depth = 3
	c.SessionID = "sess-stamp"

	before := time.Now()
	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	tokens, _ := c.Places["P:OUT"].Peek()
	tok := tokens[0]

	if tok.OriginID != "cpn-stamp" {
		t.Fatalf("OriginID = %q, want %q", tok.OriginID, "cpn-stamp")
	}
	if tok.OriginDepth != 3 {
		t.Fatalf("OriginDepth = %d, want 3", tok.OriginDepth)
	}
	if tok.OriginKind != NodeKindTool {
		t.Fatalf("OriginKind = %q, want %q", tok.OriginKind, NodeKindTool)
	}
	if tok.SessionID != "sess-stamp" {
		t.Fatalf("SessionID = %q, want %q", tok.SessionID, "sess-stamp")
	}
	if tok.Timestamp.Before(before) {
		t.Fatalf("Timestamp = %v, want >= %v", tok.Timestamp, before)
	}
}

func TestCPN_Run_SharedPlace_ReCheckCanFire(t *testing.T) {
	// Two transitions share the same input place with only one token.
	// Only the first (by sorted ID) should fire. The second re-check fails.
	places := map[string]*Place{
		"P:SHARED": NewPlace("P:SHARED", ColorString, SpaceSurface),
		"P:OUT1":   NewPlace("P:OUT1", ColorString, SpaceSurface),
		"P:OUT2":   NewPlace("P:OUT2", ColorString, SpaceSurface),
	}
	_ = places["P:SHARED"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "only-one"})

	tA := NewTransition("T:A", NodeKindTool, []string{"P:SHARED"}, []string{"P:OUT1"})
	tA.Executor = identityTool()

	tB := NewTransition("T:B", NodeKindTool, []string{"P:SHARED"}, []string{"P:OUT2"})
	tB.Executor = identityTool()

	transitions := map[string]*Transition{"T:A": tA, "T:B": tB}
	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	err := c.Run(context.Background())
	// T:A fires (sorted first), consumes the token.
	// T:B re-check fails. Next iteration: no firable, P:OUT1 has token but P:OUT2 empty.
	// P:OUT1 and P:OUT2 are both terminals → IsComplete requires both → deadlock.
	if !errors.Is(err, ErrDeadlock) {
		t.Fatalf("Run() error = %v, want ErrDeadlock", err)
	}

	// T:A should have fired.
	if places["P:OUT1"].Len() != 1 {
		t.Fatalf("P:OUT1.Len() = %d, want 1", places["P:OUT1"].Len())
	}
	// T:B should NOT have fired.
	if places["P:OUT2"].Len() != 0 {
		t.Fatalf("P:OUT2.Len() = %d, want 0", places["P:OUT2"].Len())
	}
}

// --- consumeAll Tests ---

func TestConsumeAll(t *testing.T) {
	p1 := NewPlace("P1", ColorString, SpaceSurface)
	_ = p1.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "a"})
	_ = p1.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "b"})

	p2 := NewPlace("P2", ColorJSON, SpaceComputation)
	_ = p2.Deposit(&Token{Color: ColorJSON, Space: SpaceComputation, Payload: "c"})

	places := map[string]*Place{"P1": p1, "P2": p2}

	tokens := consumeAll([]string{"P1", "P2"}, places)

	if len(tokens) != 2 {
		t.Fatalf("consumeAll returned %d tokens, want 2", len(tokens))
	}
	// First token from P1 (FIFO).
	if tokens[0].Payload != "a" {
		t.Fatalf("tokens[0].Payload = %v, want %q", tokens[0].Payload, "a")
	}
	if tokens[1].Payload != "c" {
		t.Fatalf("tokens[1].Payload = %v, want %q", tokens[1].Payload, "c")
	}

	// P1 should still have "b".
	if p1.Len() != 1 {
		t.Fatalf("P1.Len() = %d, want 1", p1.Len())
	}
	// P2 should be empty.
	if p2.Len() != 0 {
		t.Fatalf("P2.Len() = %d, want 0", p2.Len())
	}
}

// --- dispatch Tests ---

func TestDispatch_NodeKindTool(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = identityTool()

	c := NewCPN("test", "w", 0, ModeMAS, "s", places, nil)
	consumed := []Token{{Color: ColorString, Space: SpaceSurface, Payload: "x"}}

	_, err := dispatch(context.Background(), tr, c, consumed)
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if places["P:OUT"].Len() != 1 {
		t.Fatalf("P:OUT.Len() = %d, want 1", places["P:OUT"].Len())
	}
}

func TestDispatch_UnsupportedKinds(t *testing.T) {
	kinds := []NodeKind{NodeKindObserver}

	c := NewCPN("test", "w", 0, ModeMAS, "s", nil, nil)
	consumed := []Token{{Color: ColorString, Space: SpaceSurface, Payload: "x"}}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			tr := NewTransition("T:X", kind, []string{"P:IN"}, []string{"P:OUT"})
			_, err := dispatch(context.Background(), tr, c, consumed)
			if !errors.Is(err, ErrInvalidNodeKind) {
				t.Fatalf("dispatch(%s) error = %v, want ErrInvalidNodeKind", kind, err)
			}
		})
	}
}

func TestDispatch_UnknownKind(t *testing.T) {
	c := NewCPN("test", "w", 0, ModeMAS, "s", nil, nil)
	consumed := []Token{{Color: ColorString, Space: SpaceSurface, Payload: "x"}}

	tr := NewTransition("T:X", NodeKind("unknown"), []string{"P:IN"}, []string{"P:OUT"})
	_, err := dispatch(context.Background(), tr, c, consumed)
	if !errors.Is(err, ErrInvalidNodeKind) {
		t.Fatalf("dispatch(unknown) error = %v, want ErrInvalidNodeKind", err)
	}
}

// --- fireTool Tests ---

func TestFireTool_Success(t *testing.T) {
	places := map[string]*Place{
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = echoTool(func(p any) any { return p.(string) + "!" })

	c := NewCPN("cpn-1", "w", 1, ModeMAS, "sess-1", places, nil)
	consumed := []Token{{Color: ColorString, Space: SpaceSurface, Payload: "hi"}}

	_, err := fireTool(context.Background(), tr, c, consumed)
	if err != nil {
		t.Fatalf("fireTool() error = %v", err)
	}

	tokens, _ := places["P:OUT"].Peek()
	if tokens[0].Payload != "hi!" {
		t.Fatalf("Payload = %v, want %q", tokens[0].Payload, "hi!")
	}
	if tokens[0].OriginID != "cpn-1" {
		t.Fatalf("OriginID = %q, want %q", tokens[0].OriginID, "cpn-1")
	}
}

func TestFireTool_NilExecutor(t *testing.T) {
	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	// Executor is nil.

	c := NewCPN("test", "w", 0, ModeMAS, "s", nil, nil)
	consumed := []Token{{Color: ColorString, Space: SpaceSurface, Payload: "x"}}

	_, err := fireTool(context.Background(), tr, c, consumed)
	if err == nil {
		t.Fatal("fireTool() error = nil, want error for nil Executor")
	}
}

func TestFireTool_ExecutorError(t *testing.T) {
	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = alwaysFailTool()

	c := NewCPN("test", "w", 0, ModeMAS, "s", nil, nil)
	consumed := []Token{{Color: ColorString, Space: SpaceSurface, Payload: "x"}}

	_, err := fireTool(context.Background(), tr, c, consumed)
	if err == nil {
		t.Fatal("fireTool() error = nil, want error")
	}
}

func TestFireTool_NoConsumedTokens(t *testing.T) {
	tr := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = identityTool()

	c := NewCPN("test", "w", 0, ModeMAS, "s", nil, nil)

	_, err := fireTool(context.Background(), tr, c, []Token{})
	if err == nil {
		t.Fatal("fireTool() error = nil, want error for empty consumed")
	}
}

// --- Block 18: Metrics Integration Tests ---

// stubCostProvider implements CostProvider for testing.
type stubCostProvider struct {
	cost float64
}

func (s *stubCostProvider) SessionCostUSD(_ string) float64 { return s.cost }

func TestCPN_Run_RecordsMetrics(t *testing.T) {
	recorder := NewMetricsRecorder()
	c := buildSimpleCPN(echoTool(func(p any) any {
		return strings.ToUpper(p.(string))
	}))
	c.Metrics = recorder
	c.Cost = &stubCostProvider{cost: 0.03}

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}

	rec := records[0]
	if rec.CPNID != "cpn-test" {
		t.Errorf("CPNID = %q, want %q", rec.CPNID, "cpn-test")
	}
	if rec.CPNRole != "worker" {
		t.Errorf("CPNRole = %q, want %q", rec.CPNRole, "worker")
	}
	if rec.CPNDepth != 2 {
		t.Errorf("CPNDepth = %d, want 2", rec.CPNDepth)
	}
	if rec.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", rec.SessionID, "sess-1")
	}
	if !rec.Success {
		t.Error("Success = false, want true")
	}
	if rec.TransitionsFired != 1 {
		t.Errorf("TransitionsFired = %d, want 1", rec.TransitionsFired)
	}
	if rec.LLMCallCount != 0 {
		t.Errorf("LLMCallCount = %d, want 0 (tool transition)", rec.LLMCallCount)
	}
	if rec.TotalCostUSD != 0.03 {
		t.Errorf("TotalCostUSD = %f, want 0.03", rec.TotalCostUSD)
	}
	if rec.TokensProduced != 1 {
		t.Errorf("TokensProduced = %d, want 1", rec.TokensProduced)
	}
	if rec.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", rec.Duration)
	}
	if rec.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
	if rec.CompletedAt.IsZero() {
		t.Error("CompletedAt is zero")
	}
}

func TestCPN_Run_NoMetrics_NoPanic(t *testing.T) {
	c := buildSimpleCPN(identityTool())
	// c.Metrics is nil — should not panic.

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("State = %q, want %q", c.State, StateCompleted)
	}
}

func TestCPN_Run_FailedExecution_Records(t *testing.T) {
	recorder := NewMetricsRecorder()

	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	tr := NewTransition("T:FAIL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tr.Executor = alwaysFailTool()

	transitions := map[string]*Transition{"T:FAIL": tr}
	c := NewCPN("cpn-fail", "worker", 1, ModeMAS, "sess-fail", places, transitions)
	c.Metrics = recorder

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}

	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}

	rec := records[0]
	if rec.Success {
		t.Error("Success = true, want false")
	}
	if rec.CPNID != "cpn-fail" {
		t.Errorf("CPNID = %q, want %q", rec.CPNID, "cpn-fail")
	}
}

func TestCPN_Run_Timeout_RecordsMetrics(t *testing.T) {
	recorder := NewMetricsRecorder()

	places := map[string]*Place{
		"P:IN": NewPlace("P:IN", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	// Circular: output goes back to input so the CPN never completes.
	tr := NewTransition("T:LOOP", NodeKindTool, []string{"P:IN"}, []string{"P:IN"})
	tr.Executor = identityTool()

	transitions := map[string]*Transition{"T:LOOP": tr}
	c := NewCPN("cpn-timeout", "worker", 0, ModeMAS, "sess-to", places, transitions)
	c.Metrics = recorder

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.Run(ctx)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Run() error = %v, want ErrTimeout", err)
	}

	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}

	rec := records[0]
	if rec.Success {
		t.Error("Success = true, want false (timeout)")
	}
	if rec.TransitionsFired < 1 {
		t.Errorf("TransitionsFired = %d, want >= 1 (looped before timeout)", rec.TransitionsFired)
	}
}

func TestCPN_Run_NilCost_DefaultsZero(t *testing.T) {
	recorder := NewMetricsRecorder()
	c := buildSimpleCPN(identityTool())
	c.Metrics = recorder
	// c.Cost is nil — cost should default to 0.

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	if records[0].TotalCostUSD != 0.0 {
		t.Errorf("TotalCostUSD = %f, want 0.0 (nil CostProvider)", records[0].TotalCostUSD)
	}
}

func TestCPN_Run_MultipleTransitions_RecordsMetrics(t *testing.T) {
	recorder := NewMetricsRecorder()

	// P:IN -> T:A -> P:MID -> T:B -> P:OUT (linear chain, 2 transitions)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:MID": NewPlace("P:MID", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "a"})

	tA := NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:MID"})
	tA.Executor = identityTool()

	tB := NewTransition("T:B", NodeKindTool, []string{"P:MID"}, []string{"P:OUT"})
	tB.Executor = identityTool()

	transitions := map[string]*Transition{"T:A": tA, "T:B": tB}
	c := NewCPN("cpn-chain", "worker", 0, ModeMAS, "sess-chain", places, transitions)
	c.Metrics = recorder

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}

	rec := records[0]
	if rec.TransitionsFired != 2 {
		t.Errorf("TransitionsFired = %d, want 2", rec.TransitionsFired)
	}
	if rec.TokensProduced != 2 {
		t.Errorf("TokensProduced = %d, want 2", rec.TokensProduced)
	}
}

func TestCPN_Run_ValidationFails_RecordsMetrics(t *testing.T) {
	recorder := NewMetricsRecorder()

	// Invalid topology: output references non-existent place.
	places := map[string]*Place{
		"P:IN": NewPlace("P:IN", ColorString, SpaceSurface),
	}
	tr := NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:MISSING"})
	tr.Executor = identityTool()

	transitions := map[string]*Transition{"T:A": tr}
	c := NewCPN("cpn-val", "w", 0, ModeMAS, "s", places, transitions)
	c.Metrics = recorder

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want validation error")
	}

	// Validation failure happens before tracker is created, so no record.
	// The defer still runs but tracker is nil, so no panic and no record.
	records := recorder.Records()
	if len(records) != 0 {
		t.Errorf("records len = %d, want 0 (validation failure before tracker)", len(records))
	}
}
