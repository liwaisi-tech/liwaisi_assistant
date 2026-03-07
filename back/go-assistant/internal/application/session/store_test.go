package session

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir(), "/tmp/test-project")
}

func mustCreateSession(t *testing.T, store *Store, sessionID, model string) {
	t.Helper()
	if err := store.CreateSession(sessionID, model); err != nil {
		t.Fatalf("CreateSession(%q): %v", sessionID, err)
	}
}

func mustAppendMsg(t *testing.T, store *Store, sid string, msg *entity.Message) {
	t.Helper()
	if err := store.AppendMessage(sid, msg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
}

func mustAppendRewind(t *testing.T, store *Store, sid string, toIndex int) {
	t.Helper()
	if err := store.AppendRewind(sid, toIndex); err != nil {
		t.Fatalf("AppendRewind: %v", err)
	}
}

func mustAppendClear(t *testing.T, store *Store, sid string) {
	t.Helper()
	if err := store.AppendClear(sid); err != nil {
		t.Fatalf("AppendClear: %v", err)
	}
}

func TestStore_CreateSession(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "test-model")

	files, err := filepath.Glob(filepath.Join(store.baseDir, "*", "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestStore_CreateSession_FilePermissions(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "test-model")

	files, _ := filepath.Glob(filepath.Join(store.baseDir, "*", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != sessionFilePermissions {
		t.Fatalf("expected file permissions %o, got %o", sessionFilePermissions, perm)
	}
}

func TestStore_CreateSession_DirectoryPermissions(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "test-model")

	info, err := os.Stat(store.sessionDir())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != sessionDirPermissions {
		t.Fatalf("expected dir permissions %o, got %o", sessionDirPermissions, perm)
	}
}

func TestStore_AppendAndLoadMessages(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "test-model")

	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "hello", Timestamp: time.Now()})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "hi there", Timestamp: time.Now()})

	msgs, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("LoadSession returned %d messages, want 2", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "hello")
	}
	if msgs[1].Content != "hi there" {
		t.Errorf("msgs[1].Content = %q, want %q", msgs[1].Content, "hi there")
	}
}

func TestStore_LoadSession_AppliesRewind(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "test-model")

	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "msg1", Timestamp: time.Now()})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "resp1", Timestamp: time.Now()})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "msg2", Timestamp: time.Now()})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "resp2", Timestamp: time.Now()})
	mustAppendRewind(t, store, "s1", 2)

	msgs, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("after rewind, LoadSession returned %d messages, want 2", len(msgs))
	}
	if msgs[0].Content != "msg1" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "msg1")
	}
}

func TestStore_LoadSession_AppliesClear(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "test-model")

	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "msg1", Timestamp: time.Now()})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "resp1", Timestamp: time.Now()})
	mustAppendClear(t, store, "s1")
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "msg2", Timestamp: time.Now()})

	msgs, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("after clear, LoadSession returned %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "msg2" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "msg2")
	}
}

func TestStore_LoadSession_MultipleMarkers(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "test-model")

	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "m1", Timestamp: time.Now()})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "r1", Timestamp: time.Now()})
	mustAppendClear(t, store, "s1")
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "m2", Timestamp: time.Now()})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "r2", Timestamp: time.Now()})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "m3", Timestamp: time.Now()})
	mustAppendRewind(t, store, "s1", 1)

	msgs, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("after clear+rewind, LoadSession returned %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "m2" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "m2")
	}
}

func TestStore_LoadSession_NonexistentSession(t *testing.T) {
	store := newTestStore(t)
	_, err := store.LoadSession("nonexistent")
	if err == nil {
		t.Fatal("LoadSession should return error for nonexistent session")
	}
}

func TestStore_ListSessions(t *testing.T) {
	store := newTestStore(t)

	t1 := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)

	mustCreateSession(t, store, "s1", "model-a")
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "first question", Timestamp: t1})

	mustCreateSession(t, store, "s2", "model-b")
	mustAppendMsg(t, store, "s2", &entity.Message{Role: valueobject.RoleUser, Content: "second question", Timestamp: t2})
	mustAppendMsg(t, store, "s2", &entity.Message{Role: valueobject.RoleAssistant, Content: "answer", Timestamp: t2.Add(time.Second)})

	summaries, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("ListSessions returned %d summaries, want 2", len(summaries))
	}

	if summaries[0].SessionID != "s2" {
		t.Errorf("first summary SessionID = %q, want %q", summaries[0].SessionID, "s2")
	}
	if summaries[0].MessageCount != 2 {
		t.Errorf("first summary MessageCount = %d, want 2", summaries[0].MessageCount)
	}
	if summaries[0].FirstUserMsg != "second question" {
		t.Errorf("first summary FirstUserMsg = %q, want %q", summaries[0].FirstUserMsg, "second question")
	}
}

func TestStore_ListSessions_Empty(t *testing.T) {
	store := newTestStore(t)
	summaries, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("ListSessions returned %d, want 0", len(summaries))
	}
}

func TestStore_DeleteSession(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "model")

	if err := store.DeleteSession("s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	_, err := store.LoadSession("s1")
	if err == nil {
		t.Fatal("LoadSession should fail after DeleteSession")
	}
}

func TestSha256Short_Deterministic(t *testing.T) {
	h1 := sha256Short("/home/user/project")
	h2 := sha256Short("/home/user/project")
	if h1 != h2 {
		t.Errorf("sha256Short not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("sha256Short length = %d, want 16", len(h1))
	}

	h3 := sha256Short("/different/project")
	if h1 == h3 {
		t.Error("sha256Short returned same hash for different inputs")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world, this is a long message", 15, "hello world,..."},
		{"newlines replaced", "hello\nworld", 20, "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestStore_ConcurrentAppend(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "model")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := &entity.Message{
				Role:      valueobject.RoleUser,
				Content:   "concurrent msg",
				Timestamp: time.Now(),
			}
			_ = store.AppendMessage("s1", msg)
		}()
	}
	wg.Wait()

	msgs, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(msgs) != 50 {
		t.Errorf("after concurrent appends, LoadSession returned %d messages, want 50", len(msgs))
	}
}

func TestStore_ListSessions_ReflectsRewindAndClear(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "model-a")

	t1 := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "first question", Timestamp: t1})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "first answer", Timestamp: t1.Add(time.Second)})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "second question", Timestamp: t1.Add(2 * time.Second)})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "second answer", Timestamp: t1.Add(3 * time.Second)})

	mustAppendClear(t, store, "s1")
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "post-clear question", Timestamp: t1.Add(10 * time.Second)})

	summaries, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("ListSessions returned %d summaries, want 1", len(summaries))
	}

	s := summaries[0]
	if s.MessageCount != 1 {
		t.Errorf("MessageCount = %d, want 1 (after clear + 1 new message)", s.MessageCount)
	}
	if s.FirstUserMsg != "post-clear question" {
		t.Errorf("FirstUserMsg = %q, want %q", s.FirstUserMsg, "post-clear question")
	}
}

func TestStore_ListSessions_ReflectsRewind(t *testing.T) {
	store := newTestStore(t)
	mustCreateSession(t, store, "s1", "model-a")

	t1 := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "q1", Timestamp: t1})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "a1", Timestamp: t1.Add(time.Second)})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleUser, Content: "q2", Timestamp: t1.Add(2 * time.Second)})
	mustAppendMsg(t, store, "s1", &entity.Message{Role: valueobject.RoleAssistant, Content: "a2", Timestamp: t1.Add(3 * time.Second)})

	mustAppendRewind(t, store, "s1", 2)

	summaries, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("ListSessions returned %d summaries, want 1", len(summaries))
	}

	s := summaries[0]
	if s.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2 (after rewind to index 2)", s.MessageCount)
	}
}

func TestStore_SessionFile_SanitizesPathTraversal(t *testing.T) {
	store := newTestStore(t)

	tests := []struct {
		name      string
		sessionID string
	}{
		{"parent directory", "../sibling"},
		{"deep traversal", "../../etc/passwd"},
		{"absolute path", "/etc/shadow"},
		{"normal id", "chat-123456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustCreateSession(t, store, tt.sessionID, "model")

			dir := store.sessionDir()
			files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))

			for _, f := range files {
				absDir, _ := filepath.Abs(dir)
				absFile, _ := filepath.Abs(f)
				if len(absFile) < len(absDir) || absFile[:len(absDir)] != absDir {
					t.Errorf("session file %q escaped session directory %q", absFile, absDir)
				}
			}
		})
	}
}
