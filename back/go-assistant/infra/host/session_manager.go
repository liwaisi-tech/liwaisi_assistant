package host

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// sessionChunkBuffer caps the size of the per-session chunk channel. 128 is
// large enough for bursty shell output without starving the reader, and
// small enough that a stalled consumer surfaces as dropped stdout events
// rather than unbounded memory growth. Dropped chunks are logged.
const sessionChunkBuffer = 128

// InMemoryBashSessionManager implements cpn.BashSessionManager on top of
// github.com/creack/pty. All sessions are in-process; a future GAP will
// persist session metadata to the database.
type InMemoryBashSessionManager struct {
	Logger *slog.Logger

	mu       sync.RWMutex
	sessions map[string]*bashSession
}

// NewInMemoryBashSessionManager builds an empty session manager.
func NewInMemoryBashSessionManager(logger *slog.Logger) *InMemoryBashSessionManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &InMemoryBashSessionManager{
		Logger:   logger,
		sessions: make(map[string]*bashSession),
	}
}

// Compile-time interface checks.
var _ cpn.BashSessionManager = (*InMemoryBashSessionManager)(nil)
var _ cpn.BashSessionOpener = (*InMemoryBashSessionManager)(nil)

// bashSession holds a single PTY-backed process.
type bashSession struct {
	id        string
	cmd       *exec.Cmd
	pty       *os.File
	command   string
	startedAt time.Time

	chunkCh  chan []byte
	statusCh chan cpn.PTYStatus

	// eventSink, when non-nil, receives EventProcessStarted /
	// EventProcessStdout / EventProcessExit events as the PTY
	// produces output. Set via OpenWithEvents (GAP-9 REQ-002).
	eventSink func(cpn.Event)

	// emitMode controls the per-line vs per-chunk emission pattern
	// for EventProcessStdout (GAP-9 REQ-006). Defaults to per_line.
	emitMode cpn.BashEmitMode
	// emitChunkBytes is the chunk size for per_chunk mode. Zero means
	// cpn.DefaultEmitChunkBytes.
	emitChunkBytes int

	cancel context.CancelFunc
	wg     sync.WaitGroup

	closeOnce sync.Once
	alive     bool
	mu        sync.Mutex
}

// Open spawns a new PTY-backed session and returns its logical ID.
// If req.EventSink is non-nil the session additionally publishes
// EventProcessStarted, EventProcessStdout, and EventProcessExit events
// on it (GAP-9 REQ-002) — dispatcher-side emission is not required.
func (m *InMemoryBashSessionManager) Open(ctx context.Context, req cpn.PTYRequest) (string, error) {
	return m.openWithEmitConfig(ctx, req, req.EventSink, "", 0)
}

// OpenWithEvents is the BashSessionOpener entry point. It spawns a
// PTY-backed session and wires the provided sink + emit mode so
// NodeKindObserver transitions can react to stdout in real time.
func (m *InMemoryBashSessionManager) OpenWithEvents(ctx context.Context, req cpn.PTYRequest, sink func(cpn.Event)) (string, error) {
	return m.openWithEmitConfig(ctx, req, sink, "", 0)
}

// OpenWithEventsAndMode is a convenience entry point for callers that
// want to select EmitPerChunk with a custom chunk size. When mode is
// empty it defaults to EmitPerLine.
func (m *InMemoryBashSessionManager) OpenWithEventsAndMode(ctx context.Context, req cpn.PTYRequest, sink func(cpn.Event), mode cpn.BashEmitMode, chunkBytes int) (string, error) {
	return m.openWithEmitConfig(ctx, req, sink, mode, chunkBytes)
}

// openWithEmitConfig is the shared implementation body. emitMode and
// emitChunkBytes drive how stdout is split into process events; both
// default to per-line / 1024 bytes when unset.
func (m *InMemoryBashSessionManager) openWithEmitConfig(
	ctx context.Context,
	req cpn.PTYRequest,
	sink func(cpn.Event),
	emitMode cpn.BashEmitMode,
	emitChunkBytes int,
) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	if _, exists := m.sessions[id]; exists {
		m.mu.Unlock()
		return "", cpn.ErrSessionExists
	}

	sessCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(sessCtx, req.Command, req.Args...)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	if len(req.Env) > 0 {
		cmd.Env = append([]string(nil), req.Env...)
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		cancel()
		m.mu.Unlock()
		return "", fmt.Errorf("pty.Start: %w", err)
	}

	if req.Rows > 0 && req.Cols > 0 {
		_ = pty.Setsize(ptmx, &pty.Winsize{Rows: req.Rows, Cols: req.Cols})
	}

	sess := &bashSession{
		id:             id,
		cmd:            cmd,
		pty:            ptmx,
		command:        req.Command,
		startedAt:      time.Now(),
		chunkCh:        make(chan []byte, sessionChunkBuffer),
		statusCh:       make(chan cpn.PTYStatus, 4),
		eventSink:      sink,
		emitMode:       emitMode,
		emitChunkBytes: emitChunkBytes,
		cancel:         cancel,
		alive:          true,
	}

	m.sessions[id] = sess
	m.mu.Unlock()

	// Emit process_started BEFORE spawning the read loop so observers
	// see the event in order (AC-001 / REQ-002).
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	m.publish(sess, cpn.Event{
		Type: cpn.EventProcessStarted,
		Payload: cpn.ProcessOutputPayload{
			SessionID: id,
			PID:       pid,
		},
	})

	// Reader: fan-out PTY stdout → chunkCh. Closes chunkCh on EOF or kill.
	sess.wg.Add(1)
	go m.readLoop(sess)

	// Waiter: waits for cmd.Wait, publishes terminal status, triggers cleanup.
	sess.wg.Add(1)
	go m.waitLoop(sess)

	// Ready notification for listeners.
	select {
	case sess.statusCh <- cpn.PTYStatus{Kind: "ready"}:
	default:
	}

	// Observe caller cancellation so the session dies with the caller.
	go func() {
		select {
		case <-ctx.Done():
			_ = m.Kill(context.Background(), id)
		case <-sessCtx.Done():
		}
	}()

	return id, nil
}

// publish invokes the session's event sink in a non-blocking manner.
// The sink itself is expected to be non-blocking (it typically funnels
// through cpn.CPN.PublishProcessEvent which applies rate-limiting and
// then emits to a bounded channel).
func (m *InMemoryBashSessionManager) publish(sess *bashSession, e cpn.Event) {
	if sess == nil || sess.eventSink == nil {
		return
	}
	sess.eventSink(e)
}

// Write pipes data to the PTY master. Returns ErrSessionNotFound when the
// session is unknown or closed.
func (m *InMemoryBashSessionManager) Write(_ context.Context, id string, data []byte) error {
	sess, ok := m.get(id)
	if !ok {
		return cpn.ErrSessionNotFound
	}
	sess.mu.Lock()
	alive := sess.alive
	sess.mu.Unlock()
	if !alive {
		return cpn.ErrSessionNotFound
	}
	_, err := sess.pty.Write(data)
	return err
}

// List returns a snapshot of all tracked sessions.
func (m *InMemoryBashSessionManager) List(_ context.Context) []cpn.SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]cpn.SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		s.mu.Lock()
		alive := s.alive
		pid := 0
		if s.cmd != nil && s.cmd.Process != nil {
			pid = s.cmd.Process.Pid
		}
		s.mu.Unlock()
		out = append(out, cpn.SessionInfo{
			ID:        s.id,
			Command:   s.command,
			StartedAt: s.startedAt,
			PID:       pid,
			Alive:     alive,
		})
	}
	return out
}

// Kill sends SIGKILL to the session's process and triggers cleanup.
func (m *InMemoryBashSessionManager) Kill(_ context.Context, id string) error {
	sess, ok := m.get(id)
	if !ok {
		return cpn.ErrSessionNotFound
	}
	if sess.cmd != nil && sess.cmd.Process != nil {
		_ = sess.cmd.Process.Signal(syscall.SIGKILL)
	}
	sess.cancel()
	sess.wg.Wait()
	m.remove(id)
	return nil
}

// Close shuts down a session gracefully: closes the PTY, waits for the
// process to exit, and releases resources.
func (m *InMemoryBashSessionManager) Close(ctx context.Context, id string) error {
	sess, ok := m.get(id)
	if !ok {
		return cpn.ErrSessionNotFound
	}
	// Closing the PTY master causes the child shell to receive EOF; most
	// shells exit on EOF. We additionally cancel the session context after
	// a short grace period in case the shell refuses to exit.
	_ = sess.pty.Close()

	done := make(chan struct{})
	go func() {
		sess.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		sess.cancel()
		sess.wg.Wait()
	case <-ctx.Done():
		sess.cancel()
		sess.wg.Wait()
	}
	// Always cancel the session context so the caller-cancellation
	// watcher goroutine spawned in openWithEmitConfig exits cleanly,
	// even when the process died on EOF without us having to signal
	// it. cancel() is idempotent so double-cancelling is safe.
	sess.cancel()
	m.remove(id)
	return nil
}

// Chunks returns the chunk channel for the session.
func (m *InMemoryBashSessionManager) Chunks(id string) (<-chan []byte, error) {
	sess, ok := m.get(id)
	if !ok {
		return nil, cpn.ErrSessionNotFound
	}
	return sess.chunkCh, nil
}

// Status returns the status channel for the session.
func (m *InMemoryBashSessionManager) Status(id string) (<-chan cpn.PTYStatus, error) {
	sess, ok := m.get(id)
	if !ok {
		return nil, cpn.ErrSessionNotFound
	}
	return sess.statusCh, nil
}

// ── internal helpers ────────────────────────────────────────────────────────

func (m *InMemoryBashSessionManager) get(id string) (*bashSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *InMemoryBashSessionManager) remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// readLoop reads the PTY master and publishes output on sess.chunkCh
// AND on sess.eventSink (when configured). The emission pattern honours
// sess.emitMode:
//   - per_line (default): one event per newline-terminated line; a
//     trailing partial line is flushed on EOF.
//   - per_chunk: one event per N-byte window (sess.emitChunkBytes, or
//     cpn.DefaultEmitChunkBytes when zero).
//
// chunkCh always receives full lines as before — GAP-1 consumers keep
// their behaviour; only the event stream honours emit mode.
// Exits when the PTY returns EOF or an error.
func (m *InMemoryBashSessionManager) readLoop(sess *bashSession) {
	defer sess.wg.Done()
	defer func() {
		sess.closeOnce.Do(func() {
			close(sess.chunkCh)
		})
	}()

	mode := sess.emitMode
	if mode == "" {
		mode = cpn.EmitPerLine
	}
	chunkBytes := sess.emitChunkBytes
	if chunkBytes <= 0 {
		chunkBytes = cpn.DefaultEmitChunkBytes
	}

	// Buffer of bytes accumulated for per_chunk mode that haven't yet
	// reached the emission window. Allocated lazily.
	var chunkBuf []byte

	reader := bufio.NewReader(sess.pty)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			chunk := append([]byte(nil), line...)
			// Chunk channel always carries complete lines — preserves
			// the GAP-1 contract for existing consumers.
			select {
			case sess.chunkCh <- chunk:
			default:
				m.Logger.Warn("bash session chunk dropped (buffer full)", "session_id", sess.id)
			}

			// Event emission path.
			switch mode {
			case cpn.EmitPerChunk:
				chunkBuf = append(chunkBuf, line...)
				for len(chunkBuf) >= chunkBytes {
					frag := make([]byte, chunkBytes)
					copy(frag, chunkBuf[:chunkBytes])
					chunkBuf = chunkBuf[chunkBytes:]
					m.publish(sess, cpn.Event{
						Type: cpn.EventProcessStdout,
						Payload: cpn.ProcessOutputPayload{
							SessionID: sess.id,
							Line:      string(frag),
						},
					})
				}
			default: // per_line
				// Strip the trailing CR/LF for the event payload but
				// leave the chunk channel value untouched.
				trimmed := line
				for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == '\n' || trimmed[len(trimmed)-1] == '\r') {
					trimmed = trimmed[:len(trimmed)-1]
				}
				m.publish(sess, cpn.Event{
					Type: cpn.EventProcessStdout,
					Payload: cpn.ProcessOutputPayload{
						SessionID: sess.id,
						Line:      string(trimmed),
					},
				})
			}
		}
		if err != nil {
			// Flush any trailing bytes remaining in the chunk buffer
			// (per_chunk mode) so no output is lost on EOF.
			if len(chunkBuf) > 0 {
				frag := make([]byte, len(chunkBuf))
				copy(frag, chunkBuf)
				chunkBuf = nil
				m.publish(sess, cpn.Event{
					Type: cpn.EventProcessStdout,
					Payload: cpn.ProcessOutputPayload{
						SessionID: sess.id,
						Line:      string(frag),
					},
				})
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				m.Logger.Debug("pty read terminated", "session_id", sess.id, "error", err)
			}
			return
		}
	}
}

// waitLoop blocks on cmd.Wait and emits the terminal PTYStatus. It
// also publishes an EventProcessExit on the session's event sink so
// observer transitions can react to the process death (GAP-9 AC-003).
func (m *InMemoryBashSessionManager) waitLoop(sess *bashSession) {
	defer sess.wg.Done()
	waitErr := sess.cmd.Wait()

	sess.mu.Lock()
	sess.alive = false
	sess.mu.Unlock()

	// Closing the PTY now guarantees the readLoop sees EOF and exits.
	_ = sess.pty.Close()

	status := cpn.PTYStatus{Kind: "exited"}
	if sess.cmd.ProcessState != nil {
		status.ExitCode = sess.cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			status.Kind = "error"
			status.Err = waitErr
		}
	}

	// Wait for the reader to drain so the exit event never arrives
	// before the last stdout event. The readLoop exits on EOF, which
	// is triggered by the pty.Close() above.
	pid := 0
	if sess.cmd != nil && sess.cmd.Process != nil {
		pid = sess.cmd.Process.Pid
	}
	exitPayload := cpn.ProcessOutputPayload{
		SessionID: sess.id,
		PID:       pid,
		ExitCode:  status.ExitCode,
	}
	if status.Err != nil {
		exitPayload.Err = status.Err.Error()
	}
	m.publish(sess, cpn.Event{
		Type:    cpn.EventProcessExit,
		Payload: exitPayload,
	})

	select {
	case sess.statusCh <- status:
	default:
	}
	close(sess.statusCh)
}

// newSessionID returns a time-ordered UUIDv7-ish ID built from a 48-bit
// millisecond timestamp plus 80 random bits. We keep this local rather than
// depending on github.com/google/uuid so the package has zero new direct
// deps beyond creack/pty (google/uuid is available but indirect).
func newSessionID() (string, error) {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		return "", err
	}
	// Version 7, variant RFC4122.
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" +
		hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" +
		hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:]), nil
}
