package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// PersonalityToolDeps holds dependencies for personality tools.
type PersonalityToolDeps struct {
	Repo        persist.PersonalityRepository
	DefaultPers *cpn.Personality
}

// RegisterPersonalityTools registers all personality system tools in the registry.
func RegisterPersonalityTools(reg *Registry, deps *PersonalityToolDeps) error {
	tools := []struct {
		schema   *ToolSchema
		executor func(context.Context, cpn.Token) (cpn.Token, error)
	}{
		{
			schema: &ToolSchema{
				Name:         "personality.get_identity",
				Namespace:    "system",
				Description:  "Loads the user personality or returns the default. Called at boot.",
				InputColor:   cpn.ColorString,
				OutputColor:  cpn.ColorIdentity,
				Parameters:   json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string","description":"User ID to load personality for"}}}`),
				RequiresHITL: false,
				Version:      "1.0.0",
			},
			executor: makeGetIdentity(deps),
		},
		{
			schema: &ToolSchema{
				Name:         "personality.set_principle",
				Namespace:    "system",
				Description:  "Updates a single principle in the user personality.",
				InputColor:   cpn.ColorJSON,
				OutputColor:  cpn.ColorIdentity,
				Parameters:   json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string"},"kind":{"type":"string","enum":["nucleo","conducta","etica"]},"title":{"type":"string"},"description":{"type":"string"},"rules":{"type":"array","items":{"type":"string"}}},"required":["user_id","kind"]}`),
				RequiresHITL: true,
				Version:      "1.0.0",
			},
			executor: makeSetPrinciple(deps),
		},
		{
			schema: &ToolSchema{
				Name:         "personality.reset",
				Namespace:    "system",
				Description:  "Resets the user personality to defaults.",
				InputColor:   cpn.ColorString,
				OutputColor:  cpn.ColorIdentity,
				Parameters:   json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string"}},"required":["user_id"]}`),
				RequiresHITL: true,
				Version:      "1.0.0",
			},
			executor: makeReset(deps),
		},
		{
			schema: &ToolSchema{
				Name:         "personality.get_tensions",
				Namespace:    "system",
				Description:  "Returns the tension rules for the user personality.",
				InputColor:   cpn.ColorString,
				OutputColor:  cpn.ColorJSON,
				Parameters:   json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string"}}}`),
				RequiresHITL: false,
				Version:      "1.0.0",
			},
			executor: makeGetTensions(deps),
		},
		{
			schema: &ToolSchema{
				Name:         "personality.set_hierarchy",
				Namespace:    "system",
				Description:  "Reorders the principle hierarchy.",
				InputColor:   cpn.ColorJSON,
				OutputColor:  cpn.ColorIdentity,
				Parameters:   json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string"},"hierarchy":{"type":"array","items":{"type":"string","enum":["nucleo","conducta","etica"]},"minItems":3,"maxItems":3}},"required":["user_id","hierarchy"]}`),
				RequiresHITL: true,
				Version:      "1.0.0",
			},
			executor: makeSetHierarchy(deps),
		},
		{
			schema: &ToolSchema{
				Name:        "identity.about_liwaisi",
				Namespace:   "system",
				Description: "Returns information about brae (the AI agent), Liwaisi Tech, and the Liwaisi OS platform. Call this tool when the user asks anything about Liwaisi, Liwaisi Tech, brae, or the system itself.",
				InputColor:  cpn.ColorString,
				OutputColor: cpn.ColorJSON,
				Parameters:  json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string","description":"What the user wants to know: 'identity', 'creator', 'platform', 'mission', 'all'","enum":["identity","creator","platform","mission","all"]}}}`),
				Version:     "1.0.0",
			},
			executor: makeAboutLiwaisi(deps),
		},
	}

	for _, t := range tools {
		if err := reg.Register(t.schema, t.executor); err != nil {
			return fmt.Errorf("register %s: %w", t.schema.QualifiedName(), err)
		}
	}
	return nil
}

// loadPersonality retrieves a personality from the repo or falls back to default.
func loadPersonality(ctx context.Context, deps *PersonalityToolDeps, userID string) (*cpn.Personality, error) {
	if userID == "" {
		return cpn.DefaultPersonality(), nil
	}
	rec, err := deps.Repo.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load personality: %w", err)
	}
	if rec == nil {
		return cpn.DefaultPersonality(), nil
	}
	return recordToPersonality(rec)
}

// recordToPersonality converts a PersonalityRecord into a Personality.
func recordToPersonality(rec *persist.PersonalityRecord) (*cpn.Personality, error) {
	p := &cpn.Personality{
		UserID:    rec.UserID,
		Version:   rec.Version,
		UpdatedAt: rec.UpdatedAt,
	}
	if err := json.Unmarshal(rec.Principles, &p.Principles); err != nil {
		return nil, fmt.Errorf("unmarshal principles: %w", err)
	}
	if err := json.Unmarshal(rec.Hierarchy, &p.Hierarchy); err != nil {
		return nil, fmt.Errorf("unmarshal hierarchy: %w", err)
	}
	if err := json.Unmarshal(rec.Tensions, &p.Tensions); err != nil {
		return nil, fmt.Errorf("unmarshal tensions: %w", err)
	}
	return p, nil
}

// personalityToRecord converts a Personality into a PersonalityRecord.
func personalityToRecord(p *cpn.Personality) (*persist.PersonalityRecord, error) {
	principles, err := json.Marshal(p.Principles)
	if err != nil {
		return nil, fmt.Errorf("marshal principles: %w", err)
	}
	hierarchy, err := json.Marshal(p.Hierarchy)
	if err != nil {
		return nil, fmt.Errorf("marshal hierarchy: %w", err)
	}
	tensions, err := json.Marshal(p.Tensions)
	if err != nil {
		return nil, fmt.Errorf("marshal tensions: %w", err)
	}
	return &persist.PersonalityRecord{
		UserID:     p.UserID,
		Principles: principles,
		Hierarchy:  hierarchy,
		Tensions:   tensions,
		Version:    p.Version,
		UpdatedAt:  p.UpdatedAt,
	}, nil
}

// identityToken builds an IDENTITY token carrying a *Personality.
func identityToken(p *cpn.Personality) cpn.Token {
	return cpn.Token{
		Color:      cpn.ColorIdentity,
		Payload:    p,
		Space:      cpn.SpaceComputation,
		OriginKind: cpn.NodeKindTool,
		Timestamp:  time.Now(),
	}
}

// ── Tool executors ────────────────────────────────────────────────────────

func makeGetIdentity(deps *PersonalityToolDeps) func(context.Context, cpn.Token) (cpn.Token, error) {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		userID, ok := in.Payload.(string)
		if !ok {
			return cpn.Token{}, fmt.Errorf("get_identity: expected string user_id, got %T", in.Payload)
		}
		p, err := loadPersonality(ctx, deps, userID)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("get_identity: %w", err)
		}
		return identityToken(p), nil
	}
}

// setPrincipleInput is the JSON structure for set_principle.
type setPrincipleInput struct {
	UserID      string   `json:"user_id"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Rules       []string `json:"rules,omitempty"`
}

func makeSetPrinciple(deps *PersonalityToolDeps) func(context.Context, cpn.Token) (cpn.Token, error) {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		raw, err := payloadToJSON(in.Payload)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("set_principle: %w", err)
		}

		var input setPrincipleInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return cpn.Token{}, fmt.Errorf("set_principle: unmarshal input: %w", err)
		}

		kind := cpn.PrincipleKind(input.Kind)

		p, err := loadPersonality(ctx, deps, input.UserID)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("set_principle: load personality: %w", err)
		}
		p.UserID = input.UserID

		// Find the principle to update.
		var target *cpn.Principle
		for i := range p.Principles {
			if p.Principles[i].Kind == kind {
				target = &p.Principles[i]
				break
			}
		}
		if target == nil {
			return cpn.Token{}, fmt.Errorf("set_principle: unknown kind %q", input.Kind)
		}

		// Merge: only override non-zero fields.
		updated := *target
		if input.Title != "" {
			updated.Title = input.Title
		}
		if input.Description != "" {
			updated.Description = input.Description
		}
		if input.Rules != nil {
			updated.Rules = input.Rules
		}

		if err := p.ValidatePrincipleUpdate(kind, updated); err != nil {
			return cpn.Token{}, err
		}

		// Apply update.
		*target = updated

		p.Version++
		p.UpdatedAt = time.Now()

		rec, err := personalityToRecord(p)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("set_principle: %w", err)
		}
		if err := deps.Repo.Save(ctx, rec); err != nil {
			return cpn.Token{}, fmt.Errorf("set_principle: save: %w", err)
		}

		return identityToken(p), nil
	}
}

func makeReset(deps *PersonalityToolDeps) func(context.Context, cpn.Token) (cpn.Token, error) {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		userID, ok := in.Payload.(string)
		if !ok {
			return cpn.Token{}, fmt.Errorf("reset: expected string user_id, got %T", in.Payload)
		}
		if err := deps.Repo.Delete(ctx, userID); err != nil {
			return cpn.Token{}, fmt.Errorf("reset: delete: %w", err)
		}
		return identityToken(cpn.DefaultPersonality()), nil
	}
}

func makeGetTensions(deps *PersonalityToolDeps) func(context.Context, cpn.Token) (cpn.Token, error) {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		userID, ok := in.Payload.(string)
		if !ok {
			return cpn.Token{}, fmt.Errorf("get_tensions: expected string user_id, got %T", in.Payload)
		}
		p, err := loadPersonality(ctx, deps, userID)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("get_tensions: %w", err)
		}
		raw, err := json.Marshal(p.Tensions)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("get_tensions: marshal: %w", err)
		}
		return cpn.Token{
			Color:      cpn.ColorJSON,
			Payload:    json.RawMessage(raw),
			Space:      cpn.SpaceComputation,
			OriginKind: cpn.NodeKindTool,
			Timestamp:  time.Now(),
		}, nil
	}
}

// setHierarchyInput is the JSON structure for set_hierarchy.
type setHierarchyInput struct {
	UserID    string   `json:"user_id"`
	Hierarchy []string `json:"hierarchy"`
}

func makeSetHierarchy(deps *PersonalityToolDeps) func(context.Context, cpn.Token) (cpn.Token, error) {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		raw, err := payloadToJSON(in.Payload)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("set_hierarchy: %w", err)
		}

		var input setHierarchyInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return cpn.Token{}, fmt.Errorf("set_hierarchy: unmarshal input: %w", err)
		}

		if len(input.Hierarchy) != 3 {
			return cpn.Token{}, fmt.Errorf("%w: hierarchy must have exactly 3 elements", cpn.ErrInvalidHierarchy)
		}

		p, err := loadPersonality(ctx, deps, input.UserID)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("set_hierarchy: load personality: %w", err)
		}
		p.UserID = input.UserID

		p.Hierarchy = [3]cpn.PrincipleKind{
			cpn.PrincipleKind(input.Hierarchy[0]),
			cpn.PrincipleKind(input.Hierarchy[1]),
			cpn.PrincipleKind(input.Hierarchy[2]),
		}

		if err := p.ValidateHierarchy(); err != nil {
			return cpn.Token{}, err
		}

		p.Version++
		p.UpdatedAt = time.Now()

		rec, err := personalityToRecord(p)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("set_hierarchy: %w", err)
		}
		if err := deps.Repo.Save(ctx, rec); err != nil {
			return cpn.Token{}, fmt.Errorf("set_hierarchy: save: %w", err)
		}

		return identityToken(p), nil
	}
}

// aboutInput is the JSON structure for identity.about_liwaisi.
type aboutInput struct {
	Topic string `json:"topic"`
}

func makeAboutLiwaisi(deps *PersonalityToolDeps) func(context.Context, cpn.Token) (cpn.Token, error) {
	return func(_ context.Context, in cpn.Token) (cpn.Token, error) {
		id := cpn.DefaultPersonality().Identity

		// Parse topic from input (string or JSON).
		topic := "all"
		switch v := in.Payload.(type) {
		case string:
			raw, err := payloadToJSON(v)
			if err == nil {
				var input aboutInput
				if json.Unmarshal(raw, &input) == nil && input.Topic != "" {
					topic = input.Topic
				}
			}
			if topic == "all" && v != "" {
				topic = v // plain string like "creator"
			}
		}

		var result map[string]any

		switch topic {
		case "identity":
			result = map[string]any{
				"name":          id.Name,
				"acronym":       id.Acronym,
				"nature":        id.Nature,
				"gender":        id.Gender,
				"pronouns":      id.Pronouns,
				"tagline":       id.Tagline,
				"llm_disclosure": id.LLMDisclosure,
			}
		case "creator":
			result = map[string]any{
				"creator":     id.Creator,
				"creator_url": id.CreatorURL,
				"source_url":  id.SourceURL,
				"mission":     id.Mission,
			}
		case "platform":
			result = map[string]any{
				"platform":      id.Platform,
				"llm_disclosure": id.LLMDisclosure,
				"source_url":    id.SourceURL,
			}
		case "mission":
			result = map[string]any{
				"mission":     id.Mission,
				"creator":     id.Creator,
				"creator_url": id.CreatorURL,
			}
		default: // "all"
			result = map[string]any{
				"name":          id.Name,
				"acronym":       id.Acronym,
				"nature":        id.Nature,
				"gender":        id.Gender,
				"pronouns":      id.Pronouns,
				"tagline":       id.Tagline,
				"creator":       id.Creator,
				"creator_url":   id.CreatorURL,
				"source_url":    id.SourceURL,
				"platform":      id.Platform,
				"llm_disclosure": id.LLMDisclosure,
				"mission":       id.Mission,
			}
		}

		raw, err := json.Marshal(result)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("about_liwaisi: marshal: %w", err)
		}

		return cpn.Token{
			Color:      cpn.ColorJSON,
			Payload:    string(raw),
			Space:      cpn.SpaceComputation,
			OriginKind: cpn.NodeKindTool,
			Timestamp:  time.Now(),
		}, nil
	}
}

// payloadToJSON extracts raw JSON bytes from a token payload.
func payloadToJSON(payload any) ([]byte, error) {
	switch v := payload.(type) {
	case json.RawMessage:
		return v, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return json.Marshal(v)
	}
}
