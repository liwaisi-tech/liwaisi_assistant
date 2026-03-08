package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func newTestStore(t *testing.T) *FilePlanStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewFilePlanStore(dir)
	if err != nil {
		t.Fatalf("NewFilePlanStore: %v", err)
	}
	return store
}

func testDoc(sessionID, task string) *entity.PlanDocument {
	return &entity.PlanDocument{
		SessionID: sessionID,
		Task:      task,
		Team: valueobject.TeamEvaluation{
			NeedsTeam: true,
			Roles:     []valueobject.RoleSpec{{Name: "architect", Perspective: "design"}},
		},
		Graph: &entity.PlanGraph{
			Tasks: []*entity.MicroTask{
				{ID: "t1", Description: "do first"},
				{ID: "t2", Description: "do second", DependsOn: []string{"t1"}},
			},
		},
		Score:     0.85,
		Feedback:  "solid plan",
		Lifecycle: valueobject.PlanLifecycleActive,
		CreatedAt: time.Now().UTC(),
	}
}

func TestFilePlanStore_SaveAndLoad(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	doc := testDoc("session-1", "build feature")

	if err := store.Save(ctx, doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load(ctx, "session-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", loaded.SessionID, "session-1")
	}
	if loaded.Task != "build feature" {
		t.Errorf("Task = %q, want %q", loaded.Task, "build feature")
	}
	if loaded.Score != 0.85 {
		t.Errorf("Score = %f, want 0.85", loaded.Score)
	}
	if loaded.Lifecycle != valueobject.PlanLifecycleActive {
		t.Errorf("Lifecycle = %q, want active", loaded.Lifecycle)
	}
	if len(loaded.Graph.Tasks) != 2 {
		t.Errorf("Graph.Tasks = %d, want 2", len(loaded.Graph.Tasks))
	}
}

func TestFilePlanStore_Save_EmptySessionID(t *testing.T) {
	store := newTestStore(t)
	doc := &entity.PlanDocument{Task: "task"}
	if err := store.Save(context.Background(), doc); err == nil {
		t.Error("Save with empty SessionID should error")
	}
}

func TestFilePlanStore_Load_NotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Load(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Load nonexistent should error")
	}
}

func TestFilePlanStore_Save_CreatesMarkdown(t *testing.T) {
	store := newTestStore(t)
	doc := testDoc("session-md", "test markdown")
	if err := store.Save(context.Background(), doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	mdPath := filepath.Join(store.root, plansDirName, "session-md", planMDName)
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("reading markdown: %v", err)
	}
	if len(data) == 0 {
		t.Error("markdown file should not be empty")
	}
}

func TestFilePlanStore_List(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Save two plans with different creation times.
	doc1 := testDoc("session-a", "first task")
	doc1.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	doc2 := testDoc("session-b", "second task")
	doc2.CreatedAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	if err := store.Save(ctx, doc1); err != nil {
		t.Fatalf("Save doc1: %v", err)
	}
	if err := store.Save(ctx, doc2); err != nil {
		t.Fatalf("Save doc2: %v", err)
	}

	docs, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("List = %d docs, want 2", len(docs))
	}

	// Most recent first.
	if docs[0].SessionID != "session-b" {
		t.Errorf("first doc = %q, want session-b (most recent)", docs[0].SessionID)
	}
}

func TestFilePlanStore_List_EmptyDir(t *testing.T) {
	store := newTestStore(t)
	docs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("List = %d docs, want 0", len(docs))
	}
}

func TestFilePlanStore_GetActive(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	active := testDoc("active-1", "active task")
	closed := testDoc("closed-1", "closed task")
	_ = closed.Close()

	if err := store.Save(ctx, active); err != nil {
		t.Fatalf("Save active: %v", err)
	}
	if err := store.Save(ctx, closed); err != nil {
		t.Fatalf("Save closed: %v", err)
	}

	got, err := store.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if got == nil {
		t.Fatal("GetActive = nil, want active doc")
	}
	if got.SessionID != "active-1" {
		t.Errorf("GetActive.SessionID = %q, want active-1", got.SessionID)
	}
}

func TestFilePlanStore_GetActive_None(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	closed := testDoc("closed-1", "closed task")
	_ = closed.Close()
	if err := store.Save(ctx, closed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if got != nil {
		t.Errorf("GetActive = %v, want nil", got)
	}
}

func TestFilePlanStore_SaveOverwrite(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	doc := testDoc("session-overwrite", "original task")
	if err := store.Save(ctx, doc); err != nil {
		t.Fatalf("Save original: %v", err)
	}

	// Update and save again.
	doc.Score = 0.99
	doc.Task = "updated task"
	if err := store.Save(ctx, doc); err != nil {
		t.Fatalf("Save updated: %v", err)
	}

	loaded, err := store.Load(ctx, "session-overwrite")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Score != 0.99 {
		t.Errorf("Score = %f, want 0.99", loaded.Score)
	}
	if loaded.Task != "updated task" {
		t.Errorf("Task = %q, want 'updated task'", loaded.Task)
	}
}

func TestFilePlanStore_Validation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	badIDs := []string{
		"../traversal",
		"sub/dir",
		"absolute/path",
		"with\\backslash",
		"..",
		"",
	}

	for _, id := range badIDs {
		t.Run("Save_"+id, func(t *testing.T) {
			doc := &entity.PlanDocument{SessionID: id}
			if err := store.Save(ctx, doc); err == nil {
				t.Errorf("Save should fail for session ID %q", id)
			}
		})

		t.Run("Load_"+id, func(t *testing.T) {
			if _, err := store.Load(ctx, id); err == nil {
				t.Errorf("Load should fail for session ID %q", id)
			}
		})
	}
}
