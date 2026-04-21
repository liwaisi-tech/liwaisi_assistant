package cpn

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/prompts"
)

// renderSystemPrompt returns the per-invocation system prompt for an LLM
// transition. Prepends the host environment preamble (REQ-012, via
// c.HostContextFormatter) then the regional-variant preamble to the
// transition's static SystemPrompt without mutating the transition (CON-003).
// Regional preamble is skipped when SkipRegionalPreamble is set.
func renderSystemPrompt(t *Transition, c *CPN) string {
	var hostPreamble string
	if c.HostContextFormatter != nil {
		hostPreamble = c.HostContextFormatter(c)
	}

	var base string
	if t.LLMConfig != nil && t.LLMConfig.SkipRegionalPreamble {
		base = t.SystemPrompt
	} else {
		regional := prompts.PreambleFor(c.RegionalVariant)
		switch {
		case regional == "":
			base = t.SystemPrompt
		case t.SystemPrompt == "":
			base = regional
		default:
			base = regional + "\n\n" + t.SystemPrompt
		}
	}

	if hostPreamble == "" {
		return base
	}
	if base == "" {
		return hostPreamble
	}
	return hostPreamble + "\n\n" + base
}

// MaxToolCallIterations caps the agentic loop to prevent infinite cycles.
const MaxToolCallIterations = 10

// responseContainsJSONObject reports whether s contains at least one
// syntactically balanced JSON object. Used by the REQ-PAR-004 verify loop
// to decide whether a JSON-required response needs a single retry. The
// check is intentionally loose — the downstream parser (e.g.
// cmd/server/topologies.go::extractJSONObject) applies the schema-level
// filtering. This helper only distinguishes "no braces at all" from
// "something brace-shaped is present".
func responseContainsJSONObject(s string) bool {
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// fireLLM executes a NodeKindLLM transition.
//
// Flow:
//  1. Validate LLMConfig is present
//  2. Budget check (Axiom A11) — reject if estimated cost > budget
//  3. Context assembly (Axiom A12) — BuildContext + consumed tokens as user message
//  4. LLM call — Complete() via c.LLMClient
//  5. Tool-call loop — if LLM requests tools, execute and re-call
//  6. Deposit output — stamp metadata, deposit to OutputPlaces
func fireLLM(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	if t.LLMConfig == nil {
		return nil, 0, fmt.Errorf("transition %s: nil LLMConfig", t.ID)
	}

	if c.LLMClient == nil {
		return nil, 0, fmt.Errorf("transition %s: nil LLMClient on CPN", t.ID)
	}

	// Step 1: Context assembly (Axiom A12).
	ctxWindowSize := c.ContextWindowSize
	if ctxWindowSize == 0 {
		ctxWindowSize = DefaultContextWindowSize
	}
	// SkipHistory: classifier transitions must classify each message
	// independently without bias from prior conversation history.
	if t.LLMConfig.SkipHistory {
		ctxWindowSize = 0
	}
	// Snapshot history under read lock to avoid racing with concurrent appends.
	c.mu.RLock()
	historySnapshot := make([]*Message, len(c.History))
	copy(historySnapshot, c.History)
	c.mu.RUnlock()
	cw := BuildContext(renderSystemPrompt(t, c), historySnapshot, ctxWindowSize)

	// Assemble messages: system prompt + context window messages + consumed tokens.
	messages := make([]*LLMMessage, 0, 1+len(cw.Messages)+1)
	messages = append(messages, &LLMMessage{
		Role:    "system",
		Content: cw.SystemPrompt,
	})
	messages = append(messages, cw.Messages...)

	// Append consumed tokens as user message (REQ-002).
	// Skip tokens with non-user colors (JSON from classifiers, Human from HITL)
	// — the conversation history already provides context for downstream transitions.
	var userTokens []Token
	for i := range consumed {
		if consumed[i].Color == ColorString || consumed[i].Color == ColorArtifact {
			userTokens = append(userTokens, consumed[i])
		}
	}
	if len(userTokens) > 0 {
		messages = append(messages, &LLMMessage{
			Role:    "user",
			Content: formatTokenPayload(userTokens),
		})
	}

	// Resolve tool schemas from LLMTools.
	tools := make([]*LLMTool, 0, len(t.LLMTools))
	for _, toolID := range t.LLMTools {
		toolTransition, ok := c.Transitions[toolID]
		if !ok {
			continue
		}
		schema := buildToolSchema(toolTransition)
		tools = append(tools, &schema)
	}

	// Build LLM request.
	req := &LLMRequest{
		Model:       t.LLMConfig.Model,
		Messages:    messages,
		MaxTokens:   t.LLMConfig.MaxTokens,
		Temperature: t.LLMConfig.Temperature,
		Tools:       tools,
		SessionID:   c.SessionID,
		Trace:       buildTrace(t, c),
	}
	if t.LLMConfig.RequireJSON {
		req.ResponseFmt = "json_object"
	}

	// Step 2: Budget check (Axiom A11) — REQ-003/REQ-004.
	if t.LLMConfig.Budget > 0 {
		estimatedCost, err := c.LLMClient.EstimateCost(req)
		if err == nil && estimatedCost > t.LLMConfig.Budget {
			return nil, 0, fmt.Errorf("transition %s: estimated cost $%.4f exceeds budget $%.4f: %w",
				t.ID, estimatedCost, t.LLMConfig.Budget, ErrBudgetExceeded)
		}
		// If EstimateCost errors, graceful degradation — proceed with the call.
	}

	// Step 3: LLM call.
	var resp LLMResponse
	var err error
	var totalCostUSD float64

	// REQ-OBS-001: one INFO log per LLM call, structured with the identity
	// fields an operator needs to answer "which model ran for this user in
	// this session?". resolved_model is the authored Model (already written
	// by applyUserModelPreferences); the openrouter layer may map role
	// aliases further but that is observable in its own per-call log.
	slog.InfoContext(ctx, "llm call",
		"session_id", c.SessionID,
		"cpn_id", c.ID,
		"transition_id", t.ID,
		"role", t.LLMConfig.Role,
		"resolved_model", t.LLMConfig.Model,
	)

	streamOutput := t.LLMConfig.StreamOutput
	var onChunk func(string)
	if streamOutput {
		req.Stream = true
		onChunk = func(chunk string) {
			c.StreamedOutput = true
			c.emit(&Event{
				Type:           EventStreamChunk,
				TransitionID:   t.ID,
				TransitionKind: NodeKindLLM,
				Payload: StreamChunk{
					SessionID: c.SessionID,
					CPNID:     c.ID,
					CPNRole:   c.Role,
					Content:   chunk,
					Done:      false,
				},
			})
		}
		resp, err = c.LLMClient.CompleteStream(ctx, req, onChunk)
	} else {
		resp, err = c.LLMClient.Complete(ctx, req)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("transition %s: LLM call: %w", t.ID, err)
	}
	totalCostUSD += resp.CostUSD

	// REQ-PAR-004: response-format verify-and-retry. Applies ONLY to
	// non-streaming, RequireJSON+ResponseFmtRequired transitions — t-ask
	// is the canonical caller. If the first response contains no balanced
	// JSON object, we retry once with a terser directive appended to the
	// system prompt. Gemma 4 31B with reasoning mode drops JSON entirely
	// about 2% of the time; one deterministic retry recovers the call at
	// at most one extra LLM hop of cost, attributed to the same transition.
	if t.LLMConfig.ResponseFmtRequired && !streamOutput && t.LLMConfig.RequireJSON {
		if !responseContainsJSONObject(resp.Content) {
			retryReq := *req
			retryMessages := make([]*LLMMessage, len(messages))
			copy(retryMessages, messages)
			if len(retryMessages) > 0 && retryMessages[0].Role == "system" {
				sys := *retryMessages[0]
				sys.Content += "\n\nJSON only, no thinking, no fences. Your entire response MUST be a single JSON object starting with { and ending with }."
				retryMessages[0] = &sys
			}
			retryReq.Messages = retryMessages
			slog.InfoContext(ctx, "llm response-format retry",
				"session_id", c.SessionID,
				"cpn_id", c.ID,
				"transition_id", t.ID,
				"role", t.LLMConfig.Role,
				"resolved_model", t.LLMConfig.Model,
			)
			retryResp, retryErr := c.LLMClient.Complete(ctx, &retryReq)
			if retryErr == nil {
				// Cost of the retry is attributed to the same transition.
				totalCostUSD += retryResp.CostUSD
				resp = retryResp
			}
		}
	}
	// Record the actually executed model id (after role→model resolution) so
	// the executor can expose it on the transition_completed event and the UI
	// can show the real model that ran rather than the authored role alias.
	if meta := metaFromCtx(ctx); meta != nil && resp.Model != "" {
		meta.executedModel = resp.Model
	}

	// Step 4: Tool-call loop.
	var content string
	if len(resp.ToolCalls) > 0 {
		var loopCost float64
		content, loopCost, err = handleToolCalls(ctx, &resp, t, c, messages, tools, streamOutput, onChunk)
		if err != nil {
			return nil, totalCostUSD, err
		}
		totalCostUSD += loopCost
	} else {
		content = resp.Content
	}

	// Detect token limit truncation and notify user via stream.
	if (resp.StopReason == "length" || resp.StopReason == "max_tokens") && streamOutput {
		const truncationNotice = "\n\n---\n*[Response truncated — token limit reached]*"
		content += truncationNotice
		c.emit(&Event{
			Type:           EventStreamChunk,
			TransitionID:   t.ID,
			TransitionKind: NodeKindLLM,
			Payload: StreamChunk{
				SessionID: c.SessionID,
				CPNID:     c.ID,
				CPNRole:   c.Role,
				Content:   truncationNotice,
				Done:      false,
			},
		})
	}

	// Emit done sentinel if streaming was active.
	if streamOutput {
		// REQ-GAP-IND-001 / REQ-FE-005: stamp the final chunk with the resolved
		// route's registry_id + adapter hint so the frontend can render a
		// responding-model badge. Preference order for the model id: the LLM
		// adapter's echoed resp.Model (honours mid-turn fallbacks), otherwise
		// the authored LLMConfig.Model. Adapter is "openrouter" for the
		// OpenRouter client which is the only driving adapter in this repo;
		// the hint is defensive so a future direct-provider adapter can
		// override via route metadata. Earlier chunks leave the field blank
		// (REQ-GAP-IND-001: "Earlier chunks' values MAY be empty").
		c.emit(&Event{
			Type:           EventStreamChunk,
			TransitionID:   t.ID,
			TransitionKind: NodeKindLLM,
			Payload: StreamChunk{
				SessionID:       c.SessionID,
				CPNID:           c.ID,
				CPNRole:         c.Role,
				Content:         "",
				Done:            true,
				RespondingModel: formatRespondingModel(resp.Model, t.LLMConfig.Model),
			},
		})
	}

	// Append consumed input and LLM output to CPN history for downstream transitions.
	// Suppressed when EITHER:
	//   - SkipHistory: the transition reads no history AND its output has no
	//     conversational value (e.g. t-classify routing metadata), OR
	//   - SkipOutputHistory: the transition DOES read history (input side) but
	//     its output is routing metadata that must not pollute the transcript
	//     (e.g. t-ask raw questionnaire JSON consumed by t-clarify via tokens).
	// See spec-process-bugfix-a2ui-rehydration-completion.md REQ-101..107.
	if !t.LLMConfig.SkipHistory && !t.LLMConfig.SkipOutputHistory {
		c.mu.Lock()
		if len(userTokens) > 0 {
			c.History = append(c.History, &Message{
				Role:      RoleUser,
				Content:   formatTokenPayload(userTokens),
				Timestamp: time.Now(),
			})
		}
		c.History = append(c.History, &Message{
			Role:      RoleAssistant,
			Content:   content,
			CPNID:     c.ID,
			CPNRole:   c.Role,
			CPNDepth:  c.Depth,
			Timestamp: time.Now(),
		})
		c.mu.Unlock()
	}

	// Step 5: Deposit output token.
	// Output color is determined by REQ-012. Output places MUST have
	// matching Color (ColorArtifact or ColorJSON for RequireJSON).
	outputColor := inferOutputColor(t.LLMConfig.RequireJSON)
	result := Token{
		Color:       outputColor,
		Payload:     content,
		OriginID:    c.ID,
		OriginDepth: c.Depth,
		OriginKind:  NodeKindLLM,
		SessionID:   c.SessionID,
		Timestamp:   time.Now(),
	}

	// FIX-001: Build snapshot BEFORE deposit to avoid Peek race.
	outputSnaps := []TokenSnapshot{result.Snapshot()}

	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return nil, totalCostUSD, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}
		tok := result // copy per output place
		tok.Space = p.Space
		if err := p.Deposit(&tok); err != nil {
			return nil, totalCostUSD, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}

	return outputSnaps, totalCostUSD, nil
}

// handleToolCalls executes the agentic tool-call loop.
// Returns the final content string when the LLM stops requesting tools.
// prebuiltTools are the LLMTool schemas already built by fireLLM, avoiding duplicate work.
func handleToolCalls(ctx context.Context, resp *LLMResponse, t *Transition, c *CPN, messages []*LLMMessage, prebuiltTools []*LLMTool, streamOutput bool, onChunk func(string)) (content string, costUSD float64, err error) {
	// Build allowlist for O(1) lookup (PAT-003).
	allowed := make(map[string]bool, len(t.LLMTools))
	for _, id := range t.LLMTools {
		allowed[id] = true
	}

	// Reuse pre-built tool schemas from the caller (static for the loop).
	loopTools := prebuiltTools

	var loopCost float64

	for i := range MaxToolCallIterations {
		// Check context cancellation.
		select {
		case <-ctx.Done():
			return "", loopCost, ctx.Err()
		default:
		}

		// Append the assistant message with tool calls. When the LLM returns
		// multiple tool_calls in a single turn, OpenAI/Gemini require ONE
		// assistant message carrying all of them (followed by N tool
		// messages, one per tool_call_id). LLMMessage.ToolCall is singular,
		// so we emit one assistant message per tool_call and let
		// formatChatMessages serialise each as its own assistant+tool pair.
		// Carrying the tool_call on the assistant message is mandatory:
		// without it the subsequent tool/role message has no matching
		// tool_call_id and Gemini returns HTTP 400 "invalid request
		// parameters" on the re-call.
		for idx, tc := range resp.ToolCalls {
			am := &LLMMessage{Role: "assistant", ToolCall: tc}
			if idx == 0 {
				am.Content = resp.Content // preserve any text the LLM emitted
			}
			messages = append(messages, am)
		}
		if len(resp.ToolCalls) == 0 {
			messages = append(messages, &LLMMessage{Role: "assistant", Content: resp.Content})
		}

		// Execute each tool call.
		for _, tc := range resp.ToolCalls {
			// SEC-002: Enforce tool allowlist.
			if !allowed[tc.ToolName] {
				return "", loopCost, fmt.Errorf("transition %s: tool %q not in allowlist: %w",
					t.ID, tc.ToolName, ErrDisallowedTool)
			}

			toolTransition, ok := c.Transitions[tc.ToolName]
			if !ok {
				// Tool transition not found — send error back to LLM.
				messages = append(messages, &LLMMessage{
					Role:       "tool",
					ToolResult: &LLMToolResult{ToolCallID: tc.ID, Content: fmt.Sprintf("error: tool %q not found", tc.ToolName)},
				})
				continue
			}

			// Emit tool call start event for frontend ExecutionMonitor.
			startTime := time.Now()
			c.emit(&Event{
				Type:           EventTransitionStarted,
				TransitionID:   tc.ToolName,
				TransitionKind: NodeKindTool,
				SessionID:      c.SessionID,
				CPNID:          c.ID,
				CPNDepth:       c.Depth,
				CPNRole:        c.Role,
				Payload: TransitionStartedPayload{
					InputTokens: []TokenSnapshot{{
						Color:          string(ColorJSON),
						PayloadPreview: formatPayloadPreview(string(tc.Arguments)),
						OriginID:       t.ID,
						OriginKind:     string(NodeKindLLM),
					}},
					DisplayLabel: resolveLabelForTransition(c, toolTransition),
				},
				Timestamp: startTime,
			})

			// Single-gate routing: for host-gated system tools (bash_exec,
			// file_read, file_write) the HOST·HITL gate owns the approval
			// surface end-to-end. We pre-check the gate here so that
			// approve-and-remember actually persists (HandleRequiresHITL
			// writes the learned safe-pattern + stamps the first-run
			// ledger). Without this, fire_llm's generic tool-HITL card
			// would intercept the approval but never reach the gate, and
			// the subsequent gate.Check inside the executor would still
			// return ErrRequiresHITL forever.
			hostGated := false
			if gateOp, derived := DeriveHostGateOp(tc.ToolName, tc.Arguments); derived &&
				c.HostRuntime != nil && c.HostRuntime.Gate != nil && c.HostRuntime.HITLHandler != nil {
				hostGated = true
				gateCtx := WithSessionID(ctx, c.SessionID)
				if gateErr := c.HostRuntime.Gate.Check(gateCtx, gateOp); gateErr != nil {
					handled := c.HostRuntime.HITLHandler.HandleHITL(ctx, t, c, gateOp, gateErr)
					if handled != nil {
						// Denied — report back to the LLM as a tool error and
						// skip executor. Emit resolved so the UI clears.
						c.emit(&Event{
							Type:           EventHITLResolved,
							TransitionID:   t.ID,
							TransitionKind: NodeKindTool,
							SessionID:      c.SessionID,
							CPNID:          c.ID,
							Payload:        "rejected",
							Timestamp:      time.Now(),
						})
						messages = append(messages, &LLMMessage{
							Role: "tool",
							ToolResult: &LLMToolResult{
								ToolCallID: tc.ID,
								Content:    "Tool execution rejected by user",
							},
						})
						continue
					}
					c.emit(&Event{
						Type:           EventHITLResolved,
						TransitionID:   t.ID,
						TransitionKind: NodeKindTool,
						SessionID:      c.SessionID,
						CPNID:          c.ID,
						Payload:        "approved",
						Timestamp:      time.Now(),
					})
				}
			}

			// HITL gate: tools with RequiresHITL block until human approval.
			// TransitionID uses t.ID (the LLM transition) so the frontend can
			// resolve via POST /hitl/{t.ID}, which maps to t.HITLConfig.Channel.
			// Host-gated tools (handled above) skip this path to avoid a
			// duplicate approval card.
			if !hostGated && toolTransition.ToolMeta != nil && toolTransition.ToolMeta.RequiresHITL {
				// Emit the host.approval A2UI surface as a stream chunk so the
				// frontend renders HostApprovalCard with Approve/Reject buttons.
				// This mirrors the custom-surface pattern used by t-review.
				a2uiContent := buildToolApprovalA2UI(tc.ToolName, tc.Arguments)
				c.emit(&Event{
					Type:           EventStreamChunk,
					TransitionID:   t.ID,
					TransitionKind: NodeKindLLM,
					SessionID:      c.SessionID,
					CPNID:          c.ID,
					CPNDepth:       c.Depth,
					CPNRole:        c.Role,
					Payload: StreamChunk{
						SessionID: c.SessionID,
						CPNID:     c.ID,
						CPNRole:   c.Role,
						Content:   a2uiContent,
						Done:      true,
					},
					Timestamp: time.Now(),
				})

				// REQ-001/REQ-002 (spec-process-bugfix-tool-hitl-single-gate):
				// emit HITLRequestedPayload{CustomSurface:true} so the
				// integration-layer event callback (cmd/server/main.go) sees
				// the typed struct and suppresses its legacy t-review card.
				// Previously this site emitted a map[string]any, which the
				// type-switch in main.go did not match → the legacy WARN
				// branch fired and a second "Review Required" card appeared
				// after the host.approval surface (the Gate B defect).
				//
				// GUD-002: record the skip so on-call can confirm in logs
				// that the topology/builder pair are cooperating.
				slog.DebugContext(ctx, "fire_llm tool-HITL: skipping legacy t-review wiring (host.approval owns surface)",
					"transition_id", t.ID,
					"tool_name", tc.ToolName,
					"requires_hitl", true,
				)
				c.emit(&Event{
					Type:           EventHITLRequested,
					TransitionID:   t.ID,
					TransitionKind: NodeKindTool,
					SessionID:      c.SessionID,
					CPNID:          c.ID,
					CPNDepth:       c.Depth,
					CPNRole:        c.Role,
					Payload: HITLRequestedPayload{
						Prompt:        "Permiso para operar en tu máquina",
						CustomSurface: true,
					},
					Timestamp: time.Now(),
				})

				c.setState(StateWaiting)

				// FIX-HITL-PERSIST: Flush history before blocking on
				// tool-HITL approval (same pattern as fireHITL).
				if c.OnHITLWaiting != nil {
					c.mu.RLock()
					snap := make([]*Message, len(c.History))
					copy(snap, c.History)
					c.mu.RUnlock()
					c.OnHITLWaiting(snap)
				}

				if t.HITLConfig != nil && t.HITLConfig.Channel != nil {
					select {
					case <-ctx.Done():
						return "", loopCost, ctx.Err()
					case response, ok := <-t.HITLConfig.Channel:
						if !ok {
							c.setState(StateRunning)
							return "", loopCost, fmt.Errorf("transition %s: HITL channel closed unexpectedly", t.ID)
						}
						c.setState(StateRunning)
						rejected := response.Color == ColorError
						if !rejected {
							if hr, ok := response.Payload.(HITLResponse); ok {
								rejected = hr.Action == HITLReject
							} else {
								payload := fmt.Sprintf("%v", response.Payload)
								rejected = strings.EqualFold(strings.TrimSpace(payload), "reject")
							}
						}
						if rejected {
							// Tool rejected — send error to LLM.
							messages = append(messages, &LLMMessage{
								Role: "tool",
								ToolResult: &LLMToolResult{
									ToolCallID: tc.ID,
									Content:    "Tool execution rejected by user",
								},
							})
							c.emit(&Event{
								Type:           EventHITLResolved,
								TransitionID:   t.ID,
								TransitionKind: NodeKindTool,
								SessionID:      c.SessionID,
								CPNID:          c.ID,
								Payload:        "rejected",
								Timestamp:      time.Now(),
							})
							continue // Skip tool execution, proceed to next tool call.
						}
						// Approved — continue to tool execution.
						c.emit(&Event{
							Type:           EventHITLResolved,
							TransitionID:   t.ID,
							TransitionKind: NodeKindTool,
							SessionID:      c.SessionID,
							CPNID:          c.ID,
							Payload:        "approved",
							Timestamp:      time.Now(),
						})
					}
				}
				// If no HITL channel configured, log warning and proceed (graceful degradation).
			}

			// Execute the tool.
			var resultContent string
			var execErr error
			if toolTransition.Executor == nil {
				resultContent = fmt.Sprintf("error: tool %q has nil executor", tc.ToolName)
			} else {
				toolInput := Token{
					Color:   ColorJSON,
					Payload: string(tc.Arguments),
				}
				toolResult, err := toolTransition.Executor(ctx, toolInput)
				execErr = err
				if execErr != nil {
					// REQ-009: Tool errors sent back to LLM for recovery.
					resultContent = fmt.Sprintf("error: %s", execErr.Error())
				} else {
					if raw, merr := json.Marshal(toolResult.Payload); merr == nil {
						resultContent = string(raw)
					} else {
						resultContent = fmt.Sprintf("%+v", toolResult.Payload)
					}
				}
			}

			// Emit tool executed event with duration and success/failure.
			durationMs := time.Since(startTime).Milliseconds()
			namespace := ""
			if toolTransition.ToolMeta != nil {
				namespace = toolTransition.ToolMeta.Namespace
			}
			c.emit(&Event{
				Type:           EventToolExecuted,
				TransitionID:   tc.ToolName,
				TransitionKind: NodeKindTool,
				SessionID:      c.SessionID,
				CPNID:          c.ID,
				CPNDepth:       c.Depth,
				CPNRole:        c.Role,
				Payload: ToolExecutedPayload{
					ToolName:   tc.ToolName,
					Namespace:  namespace,
					DurationMs: durationMs,
					Success:    execErr == nil,
					Error:      errorString(execErr),
					Arguments:  tc.Arguments,
				},
				Timestamp: time.Now(),
			})

			messages = append(messages, &LLMMessage{
				Role:       "tool",
				ToolResult: &LLMToolResult{ToolCallID: tc.ID, Content: resultContent},
			})
		}

		// Re-call LLM with tool results.
		req := &LLMRequest{
			Model:       t.LLMConfig.Model,
			Messages:    messages,
			MaxTokens:   t.LLMConfig.MaxTokens,
			Temperature: t.LLMConfig.Temperature,
			SessionID:   c.SessionID,
			Tools:       loopTools,
			Trace:       buildTrace(t, c),
		}

		if t.LLMConfig.RequireJSON {
			req.ResponseFmt = "json_object"
		}

		var (
			newResp LLMResponse
			err     error
		)
		if streamOutput {
			req.Stream = true
			newResp, err = c.LLMClient.CompleteStream(ctx, req, onChunk)
		} else {
			newResp, err = c.LLMClient.Complete(ctx, req)
		}
		if err != nil {
			return "", loopCost, fmt.Errorf("transition %s: LLM re-call (iteration %d): %w", t.ID, i+1, err)
		}
		loopCost += newResp.CostUSD
		if meta := metaFromCtx(ctx); meta != nil && newResp.Model != "" {
			meta.executedModel = newResp.Model
		}
		resp = &newResp

		// If no more tool calls, return the content.
		if len(resp.ToolCalls) == 0 {
			return resp.Content, loopCost, nil
		}
	}

	return "", loopCost, fmt.Errorf("transition %s: %w", t.ID, ErrToolCallLoopExceeded)
}

// formatTokenPayload converts consumed tokens into a user message string.
// Single token: just the payload string. Multiple tokens: labeled sections.
func formatTokenPayload(consumed []Token) string {
	if len(consumed) == 1 {
		return fmt.Sprintf("%v", consumed[0].Payload)
	}

	var b strings.Builder
	for i := range consumed {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		fmt.Fprintf(&b, "[Token %d (%s)]: %v", i+1, consumed[i].Color, consumed[i].Payload)
	}
	return b.String()
}

// buildToolSchema converts a NodeKindTool transition to an LLMTool.
// When ToolMeta is populated (via tools.Registry.InjectIntoCPN), the
// description and JSON Schema parameters are forwarded to the LLM.
func buildToolSchema(tool *Transition) LLMTool {
	lt := LLMTool{
		Name: tool.ToolName,
	}
	if tool.ToolMeta != nil {
		lt.Description = tool.ToolMeta.Description
		lt.Parameters = tool.ToolMeta.Parameters
	}
	if lt.Description == "" {
		lt.Description = fmt.Sprintf("Execute tool: %s", tool.ToolName)
	}
	return lt
}

// buildTrace creates a TraceConfig for observability.
// Uses the transition's explicit LLMConfig.Trace if set,
// otherwise auto-populates from transition ID and CPN context.
func buildTrace(t *Transition, c *CPN) *TraceConfig {
	if t.LLMConfig.Trace != nil {
		return t.LLMConfig.Trace
	}
	return &TraceConfig{
		TraceID:        c.SessionID,
		GenerationName: t.ID,
		SpanName:       c.ID + "/" + t.ID,
	}
}

// errorString returns the error message or empty string if nil.
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// inferOutputColor returns ColorJSON if requireJSON, else ColorArtifact.
func inferOutputColor(requireJSON bool) ColorSet {
	if requireJSON {
		return ColorJSON
	}
	return ColorArtifact
}

// formatRespondingModel produces the "<registry_id> · <adapter>" label shown
// by the frontend RoundBadge (REQ-FE-005 / REQ-GAP-IND-001). Empty input yields
// an empty label so the frontend can branch on truthiness without introducing
// a placeholder like "unknown · openrouter".
//
// Preference order for the registry id:
//  1. The LLM adapter's echoed model — honours mid-turn fallbacks and is the
//     real id that answered.
//  2. The authored LLMConfig.Model — the resolved id stamped by
//     applyUserModelPreferences; stable when the adapter did not echo a value.
//
// Adapter: "openrouter" is the only driving adapter at the time of this slice
// (every Route in the registry uses ProviderAdapter="openrouter"). When direct
// adapters land, the hint can be derived from the selected Route without
// changing the callers of this helper.
func formatRespondingModel(echoed, authored string) string {
	id := strings.TrimSpace(echoed)
	if id == "" {
		id = strings.TrimSpace(authored)
	}
	if id == "" {
		return ""
	}
	return id + " · openrouter"
}

// HostApprovalInvocation is the raw-invocation sub-payload of
// HostApprovalPayload. Kept separate so the frontend's collapsed
// "Detalles técnicos" panel can render only this object.
// REQ-004 / SEC-002 — MUST reflect the exact arguments passed to the host
// adapter; divergence is a critical security bug.
type HostApprovalInvocation struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// HostApprovalPayload is the A2UI surface payload for the HOST·HITL gate.
// Shape defined in spec-process-bugfix-tool-hitl-single-gate.md §4.1.
// The outer envelope adds a `schema` discriminator and a `hostApproval`
// wrapper for frontend parity with the previous shape.
type HostApprovalPayload struct {
	Command           string                 `json:"command"`
	Invocation        HostApprovalInvocation `json:"invocation"`
	Risk              string                 `json:"risk"`
	Title             string                 `json:"title"`
	Context           string                 `json:"context,omitempty"`
	RememberAvailable bool                   `json:"rememberAvailable,omitempty"`
}

// buildToolApprovalA2UI generates the $$a2ui: host.approval payload that the
// frontend renders as a HostApprovalCard with Approve / Reject buttons.
// The payload shape is defined by HostApprovalPayload (spec §4.1).
//
// command is the human-readable shell command extracted via
// ParseShellInvocation; invocation.args echoes the raw tool-call arguments
// for the collapsed "Detalles técnicos" disclosure. When the parser flags
// an unrecognised shape the risk escalates to "caution" regardless of tool.
func buildToolApprovalA2UI(toolName string, args json.RawMessage) string {
	risk := "caution"
	if toolName == "file_read" {
		risk = "safe"
	}

	command, parsed := ParseShellInvocation(toolName, args)
	if !parsed && risk == "safe" {
		// Unknown/unrecognised shape — bump to caution per spec §9.2.
		risk = "caution"
	}
	if len(command) > 400 {
		command = command[:397] + "…"
	}

	// Preserve the raw arg bytes verbatim so the Details panel can display
	// them byte-identical to the executed invocation (SEC-002).
	invocationArgs := args
	if len(invocationArgs) == 0 {
		invocationArgs = json.RawMessage("{}")
	}

	payload := HostApprovalPayload{
		Command: command,
		Invocation: HostApprovalInvocation{
			Tool: toolName,
			Args: invocationArgs,
		},
		Risk:  risk,
		Title: "Permiso para operar en tu máquina",
	}

	// Wrap in the A2UI envelope used by the frontend HostApprovalCard parser.
	envelope := struct {
		Schema       string              `json:"schema"`
		HostApproval HostApprovalPayload `json:"hostApproval"`
	}{
		Schema:       "host.approval",
		HostApproval: payload,
	}
	raw, _ := json.Marshal(envelope)
	return "$$a2ui:" + string(raw)
}
