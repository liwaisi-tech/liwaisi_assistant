package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

const (
	sessionFilePermissions = 0o600
	sessionDirPermissions  = 0o700
)

// Store manages JSONL session files scoped to a project directory.
type Store struct {
	mu          sync.Mutex
	baseDir     string
	projectHash string
	projectPath string
}

// NewStore creates a Store rooted at baseDir, scoped to projectPath.
func NewStore(baseDir, projectPath string) *Store {
	return &Store{
		baseDir:     baseDir,
		projectHash: sha256Short(projectPath),
		projectPath: projectPath,
	}
}

// CreateSession writes the metadata record to a new JSONL file.
func (s *Store) CreateSession(sessionID, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.sessionDir()
	if err := os.MkdirAll(dir, sessionDirPermissions); err != nil {
		return fmt.Errorf("creating session directory: %w", err)
	}

	rec := Record{
		Type:        RecordMetadata,
		SessionID:   sessionID,
		ProjectHash: s.projectHash,
		ProjectPath: s.projectPath,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Model:       model,
	}

	return s.appendRecord(sessionID, &rec)
}

// AppendMessage appends a message record to the session file.
func (s *Store) AppendMessage(sessionID string, msg *entity.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := MessageToRecord(msg)
	return s.appendRecord(sessionID, &rec)
}

// AppendRewind appends a rewind marker to the session file.
func (s *Store) AppendRewind(sessionID string, toIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.appendRecord(sessionID, &Record{
		Type:      RecordRewind,
		ToIndex:   toIndex,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// AppendClear appends a clear marker to the session file.
func (s *Store) AppendClear(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.appendRecord(sessionID, &Record{
		Type:      RecordClear,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// LoadSession reads and replays a session file, returning the active messages
// after applying all rewind/clear markers.
func (s *Store) LoadSession(sessionID string) ([]entity.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.sessionFile(sessionID)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	var messages []entity.Message
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			slog.Warn("skipping malformed session record", "error", err, "session_id", sessionID)
			continue
		}

		switch rec.Type {
		case RecordMetadata:
			// Skip metadata during message loading.
		case RecordMessage:
			messages = append(messages, RecordToMessage(&rec))
		case RecordRewind:
			if rec.ToIndex >= 0 && rec.ToIndex < len(messages) {
				messages = messages[:rec.ToIndex]
			}
		case RecordClear:
			messages = nil
		default:
			slog.Warn("unknown session record type", "type", rec.Type, "session_id", sessionID)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading session file: %w", err)
	}

	return messages, nil
}

// ListSessions returns summaries for all sessions in the project directory,
// sorted by last activity (most recent first).
func (s *Store) ListSessions() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.sessionDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading session directory: %w", err)
	}

	summaries := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		summary, err := s.readSummary(filepath.Join(dir, entry.Name()))
		if err != nil {
			slog.Warn("skipping session file", "file", entry.Name(), "error", err)
			continue
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].LastActivity.After(summaries[j].LastActivity)
	})

	return summaries, nil
}

// DeleteSession removes the JSONL file for the given session.
func (s *Store) DeleteSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return os.Remove(s.sessionFile(sessionID))
}

func (s *Store) appendRecord(sessionID string, rec *Record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshaling record: %w", err)
	}

	path := s.sessionFile(sessionID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, sessionFilePermissions)
	if err != nil {
		return fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

func (s *Store) readSummary(path string) (Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return Summary{}, err
	}
	defer f.Close()

	var summary Summary
	var msgCount int
	var lastTimestamp time.Time
	var foundFirstUser bool

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}

		switch rec.Type {
		case RecordMetadata:
			summary.SessionID = rec.SessionID
			summary.Model = rec.Model
			if t, err := time.Parse(time.RFC3339, rec.CreatedAt); err == nil {
				summary.CreatedAt = t
				lastTimestamp = t
			}
		case RecordMessage:
			msgCount++
			if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil {
				lastTimestamp = t
			}
			if !foundFirstUser && rec.Role == "user" && rec.Content != "" {
				summary.FirstUserMsg = truncate(rec.Content, 60)
				foundFirstUser = true
			}
		case RecordRewind:
			if rec.ToIndex >= 0 && rec.ToIndex < msgCount {
				msgCount = rec.ToIndex
			}
			if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil {
				lastTimestamp = t
			}
		case RecordClear:
			msgCount = 0
			foundFirstUser = false
			if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil {
				lastTimestamp = t
			}
		}
	}

	summary.MessageCount = msgCount
	summary.LastActivity = lastTimestamp
	return summary, scanner.Err()
}

func (s *Store) sessionDir() string {
	return filepath.Join(s.baseDir, s.projectHash)
}

func (s *Store) sessionFile(sessionID string) string {
	clean := filepath.Base(sessionID)
	return filepath.Join(s.sessionDir(), clean+".jsonl")
}

func sha256Short(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:8])
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
