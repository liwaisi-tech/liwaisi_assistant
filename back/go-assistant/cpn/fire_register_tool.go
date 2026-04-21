package cpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// fireRegisterTool executes a NodeKindRegisterTool transition (GAP-3).
//
// Input:
//   - One token of color ColorToolManifest whose payload is a ToolManifest
//     (or a RegisterToolConfig, or a json.RawMessage carrying either). If
//     the transition's RegisterToolConfig field is non-nil, it wins over the
//     token payload.
//
// Output:
//   - One ColorArtifact token carrying ToolManifestResult
//     ({qualified_name, id, registered_at}) deposited on every OutputPlace.
//   - On registry error (duplicate, schema invalid, binary drift) the
//     executor returns an error; the standard ErrorPlace routing in
//     cpn/executor.go deposits a ColorError token there.
//
// GAP-6 hook:
//   - If BinarySHA256 is non-empty AND c.FirstRunLedger is non-nil, the
//     ledger is consulted before the registry call. Nil ledger → WARN log
//     and proceed (fall-open per REQ-034).
func fireRegisterTool(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	if c.ToolRegistry == nil {
		return nil, 0, fmt.Errorf("transition %s: CPN has no ToolRegistry wired", t.ID)
	}

	manifest, err := resolveRegisterManifest(t, consumed)
	if err != nil {
		return nil, 0, fmt.Errorf("transition %s: %w", t.ID, err)
	}

	// Stamp the authoring CPN for audit trail if the manifest didn't carry one.
	if manifest.Provenance.AuthoringCPNID == "" {
		manifest.Provenance.AuthoringCPNID = c.ID
	}
	if manifest.RegisteredBy == "" {
		manifest.RegisteredBy = c.ID
	}

	// GAP-6 first-run HITL (REQ-034). Without a ledger, we log and proceed.
	if manifest.BinarySHA256 != "" {
		if c.FirstRunLedger != nil {
			if err := c.FirstRunLedger.Approve(ctx, manifest.BinaryPath, manifest.BinarySHA256); err != nil {
				return nil, 0, fmt.Errorf("transition %s: first-run ledger rejected %s: %w", t.ID, manifest.BinaryPath, err)
			}
		} else {
			slog.WarnContext(ctx, "register_tool: binary-backed manifest without FirstRunLedger — falling open (dev-mode)",
				"transition_id", t.ID,
				"cpn_id", c.ID,
				"binary_path", manifest.BinaryPath,
				"binary_sha256", manifest.BinarySHA256,
			)
		}
	}

	result, err := c.ToolRegistry.RegisterManifest(ctx, manifest)
	if err != nil {
		return nil, 0, err
	}

	tok := Token{
		Color:       ColorArtifact,
		Payload:     result,
		Space:       SpaceComputation,
		OriginID:    c.ID,
		OriginDepth: c.Depth,
		OriginKind:  NodeKindRegisterTool,
		SessionID:   c.SessionID,
		Timestamp:   time.Now(),
	}
	snap := tok.Snapshot()

	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return nil, 0, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}
		copyTok := tok
		// If the place lives in a non-default space, align the token so
		// Deposit() does not reject it.
		if copyTok.Space == "" {
			copyTok.Space = p.Space
		}
		if err := p.Deposit(&copyTok); err != nil {
			return nil, 0, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}

	return []TokenSnapshot{snap}, 0, nil
}

// resolveRegisterManifest extracts a ToolManifest from the transition config
// (preferred) or the consumed ColorToolManifest token payload.
func resolveRegisterManifest(t *Transition, consumed []Token) (ToolManifest, error) {
	if t.RegisterToolConfig != nil {
		return t.RegisterToolConfig.ToManifest(), nil
	}
	if len(consumed) == 0 {
		return ToolManifest{}, errors.New("no input token and no RegisterToolConfig")
	}
	tok := consumed[0]
	switch payload := tok.Payload.(type) {
	case ToolManifest:
		return payload, nil
	case *ToolManifest:
		if payload == nil {
			return ToolManifest{}, errors.New("nil ToolManifest pointer in token")
		}
		return *payload, nil
	case RegisterToolConfig:
		return payload.ToManifest(), nil
	case *RegisterToolConfig:
		if payload == nil {
			return ToolManifest{}, errors.New("nil RegisterToolConfig pointer in token")
		}
		return payload.ToManifest(), nil
	case json.RawMessage:
		var m ToolManifest
		if err := json.Unmarshal(payload, &m); err != nil {
			return ToolManifest{}, fmt.Errorf("decode token payload as ToolManifest: %w", err)
		}
		return m, nil
	case []byte:
		var m ToolManifest
		if err := json.Unmarshal(payload, &m); err != nil {
			return ToolManifest{}, fmt.Errorf("decode token payload as ToolManifest: %w", err)
		}
		return m, nil
	case string:
		var m ToolManifest
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			return ToolManifest{}, fmt.Errorf("decode token payload as ToolManifest: %w", err)
		}
		return m, nil
	default:
		// Fallback — re-encode the payload and try to unmarshal.
		raw, err := json.Marshal(payload)
		if err != nil {
			return ToolManifest{}, fmt.Errorf("unsupported token payload type %T", payload)
		}
		var m ToolManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return ToolManifest{}, fmt.Errorf("decode token payload as ToolManifest: %w", err)
		}
		return m, nil
	}
}
