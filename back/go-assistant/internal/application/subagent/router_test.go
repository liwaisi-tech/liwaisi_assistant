package subagent

import (
	"fmt"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestRouter_Register_And_Get(t *testing.T) {
	tests := []struct {
		name       string
		specs      []entity.SubAgentSpec
		lookupName string
		wantFound  bool
		wantDesc   string
	}{
		{
			name: "registered spec is retrievable",
			specs: []entity.SubAgentSpec{
				{Name: "code-reviewer", Description: "Reviews code", Instruction: "You review code."},
			},
			lookupName: "code-reviewer",
			wantFound:  true,
			wantDesc:   "Reviews code",
		},
		{
			name:       "unknown name returns false",
			specs:      nil,
			lookupName: "nonexistent",
			wantFound:  false,
		},
		{
			name: "overwrite existing spec",
			specs: []entity.SubAgentSpec{
				{Name: "agent", Description: "v1", Instruction: "old"},
				{Name: "agent", Description: "v2", Instruction: "new"},
			},
			lookupName: "agent",
			wantFound:  true,
			wantDesc:   "v2",
		},
		{
			name: "multiple specs are independent",
			specs: []entity.SubAgentSpec{
				{Name: "alpha", Description: "first", Instruction: "a"},
				{Name: "beta", Description: "second", Instruction: "b"},
			},
			lookupName: "beta",
			wantFound:  true,
			wantDesc:   "second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter()
			for i := range tt.specs {
				router.Register(&tt.specs[i])
			}

			got, ok := router.Get(tt.lookupName)
			if ok != tt.wantFound {
				t.Fatalf("Get(%q) found = %v, want %v", tt.lookupName, ok, tt.wantFound)
			}
			if tt.wantFound && got.Description != tt.wantDesc {
				t.Errorf("Get(%q).Description = %q, want %q", tt.lookupName, got.Description, tt.wantDesc)
			}
		})
	}
}

func TestRouter_List(t *testing.T) {
	tests := []struct {
		name      string
		specs     []entity.SubAgentSpec
		wantNames []string
	}{
		{
			name:      "empty router returns empty list",
			specs:     nil,
			wantNames: []string{},
		},
		{
			name: "single spec",
			specs: []entity.SubAgentSpec{
				{Name: "solo", Instruction: "do stuff"},
			},
			wantNames: []string{"solo"},
		},
		{
			name: "multiple specs sorted alphabetically",
			specs: []entity.SubAgentSpec{
				{Name: "zeta", Instruction: "z"},
				{Name: "alpha", Instruction: "a"},
				{Name: "mu", Instruction: "m"},
			},
			wantNames: []string{"alpha", "mu", "zeta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter()
			for i := range tt.specs {
				router.Register(&tt.specs[i])
			}

			got := router.List()
			if len(got) != len(tt.wantNames) {
				t.Fatalf("List() returned %d specs, want %d", len(got), len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if got[i].Name != want {
					t.Errorf("List()[%d].Name = %q, want %q", i, got[i].Name, want)
				}
			}
		})
	}
}

func TestRouter_List_PreservesFullSpec(t *testing.T) {
	router := NewRouter()
	router.Register(&entity.SubAgentSpec{
		Name:        "researcher",
		Description: "Research agent",
		Instruction: "You research topics.",
		ModelTier:   valueobject.ModelTierFast,
		MaxTurns:    5,
	})

	specs := router.List()
	if len(specs) != 1 {
		t.Fatalf("List() returned %d specs, want 1", len(specs))
	}

	s := specs[0]
	if s.Name != "researcher" {
		t.Errorf("Name = %q, want %q", s.Name, "researcher")
	}
	if s.ModelTier != valueobject.ModelTierFast {
		t.Errorf("ModelTier = %q, want %q", s.ModelTier, valueobject.ModelTierFast)
	}
	if s.MaxTurns != 5 {
		t.Errorf("MaxTurns = %d, want 5", s.MaxTurns)
	}
}

func TestRouter_Concurrent(t *testing.T) {
	router := NewRouter()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		idx := i
		wg.Add(3)
		go func() {
			defer wg.Done()
			router.Register(&entity.SubAgentSpec{
				Name:        fmt.Sprintf("agent-%d", idx),
				Instruction: "concurrent test",
			})
		}()
		go func() {
			defer wg.Done()
			router.Get(fmt.Sprintf("agent-%d", idx))
		}()
		go func() {
			defer wg.Done()
			router.List()
		}()
	}
	wg.Wait()
}
