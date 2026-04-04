//go:build integration

package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func makeTestEvent(id, sessionID, cpnID, typ string) *persist.EventRecord {
	return &persist.EventRecord{
		ID:        id,
		Type:      typ,
		SessionID: sessionID,
		CPNID:     cpnID,
		CPNRole:   "coordinator",
		Timestamp: time.Now(),
	}
}

func TestPostgresEventRepository_Append(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewEventRepository(pool)
	ctx := context.Background()

	t.Run("Single", func(t *testing.T) {
		err := repo.Append(ctx, makeTestEvent("e1", "s1", "c1", "transition_fired"))
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
	})

	t.Run("Batch", func(t *testing.T) {
		events := make([]*persist.EventRecord, 10)
		for i := range events {
			events[i] = makeTestEvent(fmt.Sprintf("eb%d", i), "s2", "c1", "transition_fired")
		}
		err := repo.Append(ctx, events...)
		if err != nil {
			t.Fatalf("Append batch: %v", err)
		}
	})

	t.Run("Nil_event", func(t *testing.T) {
		err := repo.Append(ctx, nil)
		if err == nil {
			t.Fatal("should error on nil event")
		}
	})
}

func TestPostgresEventRepository_QueryBySession(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewEventRepository(pool)
	ctx := context.Background()

	for i := range 5 {
		_ = repo.Append(ctx, makeTestEvent(fmt.Sprintf("qs%d", i), "sess-q", "c1", "t1"))
	}
	_ = repo.Append(ctx, makeTestEvent("qs-other", "sess-other", "c1", "t1"))

	page, err := repo.QueryBySession(ctx, "sess-q", nil)
	if err != nil {
		t.Fatalf("QueryBySession: %v", err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("want 5 items, got %d", len(page.Items))
	}
}

func TestPostgresEventRepository_Pagination(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewEventRepository(pool)
	ctx := context.Background()

	for i := range 10 {
		e := makeTestEvent(fmt.Sprintf("pg%d", i), "sess-pg", "c1", "t1")
		e.Timestamp = time.Now().Add(time.Duration(i) * time.Millisecond)
		_ = repo.Append(ctx, e)
	}

	opts := &persist.EventQueryOpts{Limit: 3}
	page1, err := repo.QueryBySession(ctx, "sess-pg", opts)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Items) != 3 {
		t.Fatalf("page1: want 3, got %d", len(page1.Items))
	}
	if !page1.HasMore {
		t.Fatal("page1: want HasMore=true")
	}

	opts2 := &persist.EventQueryOpts{Limit: 3, Cursor: page1.NextCursor}
	page2, err := repo.QueryBySession(ctx, "sess-pg", opts2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Items) != 3 {
		t.Fatalf("page2: want 3, got %d", len(page2.Items))
	}

	// Verify no duplicates
	seen := map[string]bool{}
	for _, e := range page1.Items {
		seen[e.ID] = true
	}
	for _, e := range page2.Items {
		if seen[e.ID] {
			t.Fatalf("duplicate event %s across pages", e.ID)
		}
	}
}

func TestPostgresEventRepository_Count(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewEventRepository(pool)
	ctx := context.Background()

	for i := range 5 {
		_ = repo.Append(ctx, makeTestEvent(fmt.Sprintf("cnt%d", i), "sess-cnt", "c1", "t1"))
	}

	count, err := repo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-cnt"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 5 {
		t.Fatalf("want 5, got %d", count)
	}
}

func TestEventBatcher(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewEventRepository(pool)
	ctx := context.Background()

	t.Run("FlushOnSize", func(t *testing.T) {
		batcher := NewEventBatcher(repo, 5, 10*time.Second)
		for i := range 5 {
			_ = batcher.Submit(ctx, makeTestEvent(fmt.Sprintf("bs%d", i), "sess-bs", "c1", "t1"))
		}
		time.Sleep(100 * time.Millisecond)
		count, _ := repo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-bs"})
		if count != 5 {
			t.Fatalf("want 5 flushed, got %d", count)
		}
		_ = batcher.Close(ctx)
	})

	t.Run("FlushOnInterval", func(t *testing.T) {
		batcher := NewEventBatcher(repo, 100, 200*time.Millisecond)
		for i := range 3 {
			_ = batcher.Submit(ctx, makeTestEvent(fmt.Sprintf("bi%d", i), "sess-bi", "c1", "t1"))
		}
		time.Sleep(500 * time.Millisecond)
		count, _ := repo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-bi"})
		if count != 3 {
			t.Fatalf("want 3 flushed on interval, got %d", count)
		}
		_ = batcher.Close(ctx)
	})

	t.Run("CloseFlushesRemaining", func(t *testing.T) {
		batcher := NewEventBatcher(repo, 100, 10*time.Second)
		for i := range 7 {
			_ = batcher.Submit(ctx, makeTestEvent(fmt.Sprintf("bc%d", i), "sess-bc", "c1", "t1"))
		}
		_ = batcher.Close(ctx)
		count, _ := repo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-bc"})
		if count != 7 {
			t.Fatalf("want 7 after close, got %d", count)
		}
	})

	t.Run("Concurrent", func(t *testing.T) {
		batcher := NewEventBatcher(repo, 10, 100*time.Millisecond)
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = batcher.Submit(ctx, makeTestEvent(fmt.Sprintf("bcc%d", i), "sess-bcc", "c1", "t1"))
			}(i)
		}
		wg.Wait()
		_ = batcher.Close(ctx)
		count, _ := repo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-bcc"})
		if count != 20 {
			t.Fatalf("want 20 concurrent, got %d", count)
		}
	})
}
