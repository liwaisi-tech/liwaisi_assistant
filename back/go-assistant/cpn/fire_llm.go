package cpn

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MaxToolCallIterations caps the agentic loop to prevent infinite cycles.
const MaxToolCallIterations = 10

// fireLLM executes a NodeKindLLM transition.
//
// Flow:
//  1. Validate LLMConfig is present
//  2. Budget check (Axiom A11) — reject if estimated cost > budget
//  3. Context assembly (Axiom A12) — BuildContext + consumed tokens as user message
//  4. LLM call — Complete() via c.LLMClient
//  5. Tool-call loop — if LLM requests tools, execute and re-call
//  6. Deposit output — stamp metadata, deposit to OutputPlaces
func fireLLM(ctx context.Context, t *Transition, c *CPN, consumed []Token) (float64, error) {
	if t.LLMConfig == nil {
		return 0, fmt.Errorf("transition %s: nil LLMConfig", t.ID)
	}

	if c.LLMClient == nil {
		return 0, fmt.Errorf("transition %s: nil LLMClient on CPN", t.ID)
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
	cw := BuildContext(t.SystemPrompt, historySnapshot, ctxWindowSize)

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
			return 0, fmt.Errorf("transition %s: estimated cost $%.4f exceeds budget $%.4f: %w",
				t.ID, estimatedCost, t.LLMConfig.Budget, ErrBudgetExceeded)
		}
		// If EstimateCost errors, graceful degradation — proceed with the call.
	}

	// Step 3: LLM call.
	var resp LLMResponse
	var err error
	var totalCostUSD float64

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
		return 0, fmt.Errorf("transition %s: LLM call: %w", t.ID, err)
	}
	totalCostUSD += resp.CostUSD

	// Step 4: Tool-call loop.
	var content string
	if len(resp.ToolCalls) > 0 {
		var loopCost float64
		content, loopCost, err = handleToolCalls(ctx, &resp, t, c, messages, tools, streamOutput, onChunk)
		if err != nil {
			return totalCostUSD, err
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
		c.emit(&Event{
			Type:           EventStreamChunk,
			TransitionID:   t.ID,
			TransitionKind: NodeKindLLM,
			Payload: StreamChunk{
				SessionID: c.SessionID,
				CPNID:     c.ID,
				CPNRole:   c.Role,
				Content:   "",
				Done:      true,
			},
		})
	}

	// Append consumed input and LLM output to CPN history for downstream transitions.
	// Skip when SkipHistory is true — classifier output is routing metadata,
	// not conversational content that downstream transitions need in history.
	if !t.LLMConfig.SkipHistory {
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

	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return totalCostUSD, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}
		tok := result // copy per output place
		tok.Space = p.Space
		if err := p.Deposit(&tok); err != nil {
			return totalCostUSD, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}

	return totalCostUSD, nil
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

		// Append the assistant message with tool calls.
		assistantMsg := &LLMMessage{
			Role:    "assistant",
			Content: resp.Content,
		}
		messages = append(messages, assistantMsg)

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
				},
				Timestamp: startTime,
			})

			// HITL gate: tools with RequiresHITL block until human approval.
			if toolTransition.ToolMeta != nil && toolTransition.ToolMeta.RequiresHITL {
				c.emit(&Event{
					Type:           EventHITLRequested,
					TransitionID:   tc.ToolName,
					TransitionKind: NodeKindTool,
					SessionID:      c.SessionID,
					CPNID:          c.ID,
					CPNDepth:       c.Depth,
					CPNRole:        c.Role,
					Payload: map[string]any{
						"tool_name":    tc.ToolName,
						"arguments":    string(tc.Arguments),
						"tool_call_id": tc.ID,
						"description":  toolTransition.ToolMeta.Description,
					},
					Timestamp: time.Now(),
				})

				c.setState(StateWaiting)

				if t.HITLConfig != nil && t.HITLConfig.Channel != nil {
					select {
					case <-ctx.Done():
						return "", loopCost, ctx.Err()
					case response := <-t.HITLConfig.Channel:
						c.setState(StateRunning)
						payload := fmt.Sprintf("%v", response.Payload)
						if strings.EqualFold(strings.TrimSpace(payload), "reject") ||
							response.Color == ColorError {
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
								TransitionID:   tc.ToolName,
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
							TransitionID:   tc.ToolName,
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
					resultContent = fmt.Sprintf("%v", toolResult.Payload)
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
