// Package filesystem provides file-based adapters for domain output ports.
package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	plansDirName = "plans"
	planFileName = "plan.json"
	planMDName   = "plan.md"
	dirPerm      = 0o750
	filePerm     = 0o640
)

// compile-time interface check.
var _ output.PlanStore = (*FilePlanStore)(nil)

// FilePlanStore implements PlanStore by writing plan documents as JSON files
// in a directory tree: {root}/plans/{session-id}/plan.json.
// A human-readable Markdown version is also written alongside.
type FilePlanStore struct {
	root string
}

// NewFilePlanStore creates a FilePlanStore rooted at the given directory.
// The plans subdirectory is created if it does not exist.
func NewFilePlanStore(root string) (*FilePlanStore, error) {
	dir := filepath.Join(root, plansDirName)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("creating plans directory: %w", err)
	}
	return &FilePlanStore{root: root}, nil
}

// Save persists a PlanDocument as both JSON and Markdown.
func (s *FilePlanStore) Save(_ context.Context, doc *entity.PlanDocument) error {
	if err := validateSessionID(doc.SessionID); err != nil {
		return fmt.Errorf("plan store: %w", err)
	}

	dir := filepath.Join(s.root, plansDirName, doc.SessionID)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("creating plan directory: %w", err)
	}

	// Write JSON.
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling plan: %w", err)
	}
	jsonPath := filepath.Join(dir, planFileName)
	if err := os.WriteFile(jsonPath, data, filePerm); err != nil {
		return fmt.Errorf("writing plan JSON: %w", err)
	}

	// Write Markdown.
	mdPath := filepath.Join(dir, planMDName)
	if err := os.WriteFile(mdPath, []byte(doc.ToMarkdown()), filePerm); err != nil {
		return fmt.Errorf("writing plan Markdown: %w", err)
	}

	return nil
}

// Load retrieves a PlanDocument by session ID.
func (s *FilePlanStore) Load(_ context.Context, sessionID string) (*entity.PlanDocument, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, fmt.Errorf("plan store: %w", err)
	}
	jsonPath := filepath.Join(s.root, plansDirName, sessionID, planFileName)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("plan %q not found", sessionID)
		}
		return nil, fmt.Errorf("reading plan: %w", err)
	}

	var doc entity.PlanDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing plan: %w", err)
	}
	return &doc, nil
}

// List returns all persisted plan documents, ordered by creation time (most recent first).
func (s *FilePlanStore) List(_ context.Context) ([]*entity.PlanDocument, error) {
	dir := filepath.Join(s.root, plansDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing plans: %w", err)
	}

	docs := make([]*entity.PlanDocument, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := validateSessionID(entry.Name()); err != nil {
			continue // skip invalid directory names
		}
		jsonPath := filepath.Join(dir, entry.Name(), planFileName)
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			continue // skip entries without valid plan files
		}

		var doc entity.PlanDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			continue
		}
		docs = append(docs, &doc)
	}

	// Sort by creation time, most recent first.
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].CreatedAt.After(docs[j].CreatedAt)
	})

	return docs, nil
}

// GetActive returns the currently active plan, or nil if none exists.
func (s *FilePlanStore) GetActive(ctx context.Context) (*entity.PlanDocument, error) {
	docs, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if doc.Lifecycle == valueobject.PlanLifecycleActive {
			return doc, nil
		}
	}
	return nil, nil
}

func validateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("session ID is required")
	}
	// Basic path traversal protection.
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid session ID: %q", id)
	}
	return nil
}
