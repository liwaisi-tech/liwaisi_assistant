package app

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// SkillKind identifies the category of a skill entry in the manifest.
type SkillKind string

const (
	SkillKindBuiltin SkillKind = "builtin"
	SkillKindFlow    SkillKind = "flow"
	SkillKindTool    SkillKind = "tool"
	SkillKindHostCap SkillKind = "host_capability"
)

// Skill is one entry in the manifest.
type Skill struct {
	ID                string    `json:"id"`
	Kind              SkillKind `json:"kind"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Version           string    `json:"version,omitempty"`
	Origin            string    `json:"origin"`
	Deprecated        bool      `json:"deprecated"`
	ProvenanceSummary string    `json:"provenance_summary,omitempty"`
}

// Manifest is the full aggregated snapshot of brae capabilities.
type Manifest struct {
	BuiltAt time.Time      `json:"built_at"`
	Skills  []Skill        `json:"skills"`
	Counts  map[string]int `json:"counts"`
}

const manifestTTL = 60 * time.Second

// SkillManifestService aggregates built-in topologies, registered tools, and
// host capabilities into a single Manifest. It caches the result for 60 s and
// can be invalidated early on mutation events.
type SkillManifestService struct {
	toolRepo     persist.ToolRegistryRepository
	capRepo      persist.HostCapabilityRepository
	hostID       string
	builtinNames []string

	mu          sync.Mutex
	cached      *Manifest
	cachedAt    time.Time
}

// NewSkillManifestService creates the service. builtinNames is the ordered list
// of built-in topology names to surface (e.g. "classifier", "host-discovery").
// hostID is the machine identifier used to query the capability repository.
func NewSkillManifestService(
	toolRepo persist.ToolRegistryRepository,
	capRepo persist.HostCapabilityRepository,
	hostID string,
	builtinNames []string,
) *SkillManifestService {
	return &SkillManifestService{
		toolRepo:     toolRepo,
		capRepo:      capRepo,
		hostID:       hostID,
		builtinNames: builtinNames,
	}
}

// Invalidate clears the cached manifest so the next Build call rebuilds.
func (s *SkillManifestService) Invalidate(_ string) {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// Build returns the manifest, using the cache when still fresh.
func (s *SkillManifestService) Build(ctx context.Context) (Manifest, error) {
	s.mu.Lock()
	if s.cached != nil && time.Since(s.cachedAt) < manifestTTL {
		m := *s.cached
		s.mu.Unlock()
		return m, nil
	}
	s.mu.Unlock()

	m, err := s.build(ctx)
	if err != nil {
		return Manifest{}, err
	}

	s.mu.Lock()
	s.cached = &m
	s.cachedAt = time.Now()
	s.mu.Unlock()

	return m, nil
}

func (s *SkillManifestService) build(ctx context.Context) (Manifest, error) {
	var skills []Skill

	// 1. Built-in topologies — always first for prompt-cache stability.
	for _, name := range s.builtinNames {
		skills = append(skills, Skill{
			ID:     name,
			Kind:   SkillKindBuiltin,
			Name:   name,
			Origin: "builtin",
		})
	}

	// 2. Registered tools (all origins, including deprecated).
	tools, err := s.toolRepo.ListAll(ctx)
	if err != nil {
		return Manifest{}, fmt.Errorf("skill manifest: list tools: %w", err)
	}
	for _, t := range tools {
		kind := SkillKindTool
		if t.Origin == "agent-authored" {
			kind = SkillKindTool
		}
		prov := ""
		if t.Provenance.ForgeRunID != "" {
			prov = "forge:" + t.Provenance.ForgeRunID
		}
		skills = append(skills, Skill{
			ID:                t.QualifiedName(),
			Kind:              kind,
			Name:              t.Name,
			Description:       t.HelpText,
			Version:           t.Version,
			Origin:            t.Origin,
			Deprecated:        t.Deprecated,
			ProvenanceSummary: prov,
		})
	}

	// 3. Host capabilities (optional — no snapshot is not an error).
	if s.capRepo != nil && s.hostID != "" {
		snap, err := s.capRepo.LatestForHost(ctx, s.hostID)
		if err == nil {
			for _, c := range snap.Capabilities {
				val := "no"
				if c.Satisfied {
					val = "yes"
				}
				skills = append(skills, Skill{
					ID:          c.Name,
					Kind:        SkillKindHostCap,
					Name:        c.Name,
					Description: val,
					Origin:      "host",
				})
			}
		}
	}

	counts := map[string]int{}
	for _, sk := range skills {
		counts[string(sk.Kind)]++
	}

	return Manifest{
		BuiltAt: time.Now(),
		Skills:  skills,
		Counts:  counts,
	}, nil
}

// BuildCompact returns a Markdown summary of the manifest suitable for LLM
// system-prompt injection, truncated to sizeCap bytes. Truncation drops host
// capabilities first, then agent-authored tools, then user flows — built-ins
// are never dropped.
func (s *SkillManifestService) BuildCompact(ctx context.Context, sizeCap int) (string, error) {
	m, err := s.Build(ctx)
	if err != nil {
		return "", err
	}

	full := renderCompact(m)
	if len(full) <= sizeCap || sizeCap <= 0 {
		return full, nil
	}

	// Progressive truncation: drop host caps, then deprecated tools, then
	// agent-authored tools, then non-builtin flows.
	dropOrder := []SkillKind{SkillKindHostCap, SkillKindTool, SkillKindFlow}
	kept := make([]Skill, len(m.Skills))
	copy(kept, m.Skills)

	for _, dropKind := range dropOrder {
		var next []Skill
		dropped := false
		for i := len(kept) - 1; i >= 0; i-- {
			sk := kept[i]
			if !dropped && sk.Kind == dropKind && sk.Origin != "builtin" {
				dropped = true
				continue
			}
			next = append([]Skill{sk}, next...)
		}
		kept = next
		m2 := Manifest{BuiltAt: m.BuiltAt, Skills: kept, Counts: m.Counts}
		if candidate := renderCompact(m2); len(candidate) <= sizeCap {
			return candidate, nil
		}
	}

	// Last resort: hard-truncate the rendered string.
	return full[:sizeCap], nil
}

func renderCompact(m Manifest) string {
	var buf bytes.Buffer

	buf.WriteString("## Flows\n")
	for _, sk := range m.Skills {
		if sk.Kind != SkillKindBuiltin && sk.Kind != SkillKindFlow {
			continue
		}
		if sk.Deprecated {
			continue
		}
		desc := sk.Description
		if desc == "" {
			desc = sk.Name
		}
		fmt.Fprintf(&buf, "- `%s` — %s\n", sk.Name, desc)
	}

	buf.WriteString("\n## Tools\n")
	for _, sk := range m.Skills {
		if sk.Kind != SkillKindTool {
			continue
		}
		if sk.Deprecated {
			continue
		}
		qn := sk.ID
		if sk.Version != "" {
			qn = sk.Name + "@" + sk.Version
		}
		desc := sk.Description
		if desc == "" {
			desc = sk.Name
		}
		fmt.Fprintf(&buf, "- `%s` — %s\n", qn, desc)
	}

	buf.WriteString("\n## Host capabilities\n")
	for _, sk := range m.Skills {
		if sk.Kind != SkillKindHostCap {
			continue
		}
		fmt.Fprintf(&buf, "- %s: %s\n", sk.Name, sk.Description)
	}

	return buf.String()
}
