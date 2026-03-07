package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/subagent"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// Compile-time interface verification.
var _ input.AgentService = (*agentService)(nil)

const defaultToolLoopTimeout = 15 * time.Minute

// AgentConfig holds configuration for the agent service.
type AgentConfig struct {
	Model             string
	SystemPrompt      string
	Temperature       float64
	MaxTokens         int
	HistoryLimit      int
	MaxToolIterations int
	ToolLoopTimeout   time.Duration
}

type agentService struct {
	client            output.LLMClient
	memory            *memory.ConversationMemory
	model             string
	systemPrompt      string
	temperature       float64
	maxTokens         int
	historyLimit      int
	maxToolIterations int
	toolLoopTimeout   time.Duration
	toolExecutor      tool.Executor
	sessionStore      *tool.SessionRegistryStore
	tracer            trace.Tracer
}

// Option configures optional agentService behavior.
type Option func(*agentService)

// WithSessionStore enables JIT tool loading by providing a per-session
// registry store. When set, each Chat session resolves its own
// ActiveRegistry from the store, overriding the default executor.
func WithSessionStore(store *tool.SessionRegistryStore) Option {
	return func(s *agentService) {
		if store != nil {
			s.sessionStore = store
		}
	}
}

// NewAgentService creates an AgentService backed by the given LLM client.
// executor may be nil (tools disabled) or any tool.Executor (Registry,
// ActiveRegistry, etc.). If mem is nil, a new ConversationMemory is
// created internally.
func NewAgentService(client output.LLMClient, cfg AgentConfig, executor tool.Executor, mem *memory.ConversationMemory, opts ...Option) input.AgentService {
	limit := cfg.HistoryLimit
	if limit <= 0 {
		limit = 100
	}
	maxIter := cfg.MaxToolIterations
	if maxIter <= 0 {
		maxIter = 50
	}
	timeout := cfg.ToolLoopTimeout
	if timeout <= 0 {
		timeout = defaultToolLoopTimeout
	}

	if mem == nil {
		mem = memory.NewConversationMemory()
	}

	svc := &agentService{
		client:            client,
		memory:            mem,
		model:             cfg.Model,
		systemPrompt:      cfg.SystemPrompt,
		temperature:       cfg.Temperature,
		maxTokens:         cfg.MaxTokens,
		historyLimit:      limit,
		maxToolIterations: maxIter,
		toolLoopTimeout:   timeout,
		toolExecutor:      executor,
		tracer:            otel.Tracer("go-assistant/agent"),
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// Chat sends a user message within a session and returns a channel that
// streams tool progress events followed by the assistant's response tokens.
// The channel is returned immediately; both the tool loop and streaming
// phases write to it from a background goroutine.
func (s *agentService) Chat(ctx context.Context, sessionID, userMessage string) (<-chan valueobject.StreamChunk, error) {
	ctx, span := s.tracer.Start(ctx, "agentService.Chat",
		trace.WithAttributes(
			attribute.String("session.id", sessionID),
			attribute.String("agent.model", s.model),
			attribute.Int("message.length", len(userMessage)),
		))

	slog.InfoContext(ctx, "chat request started",
		"session_id", sessionID,
		"model", s.model,
		"message_length", len(userMessage),
	)

	s.memory.GetOrCreate(sessionID, s.systemPrompt)
	userMsg := entity.NewMessage(valueobject.RoleUser, userMessage)
	s.memory.Append(sessionID, &userMsg)

	messages := s.buildMessages(sessionID)

	outCh := make(chan valueobject.StreamChunk)
	go s.chatPipeline(ctx, span, sessionID, messages, outCh)
	return outCh, nil
}

// chatPipeline runs both the tool loop (Phase 1) and streaming (Phase 2)
// in sequence, writing all events to outCh. It owns the channel lifecycle.
func (s *agentService) chatPipeline(
	ctx context.Context,
	span trace.Span,
	sessionID string,
	messages []entity.Message,
	outCh chan<- valueobject.StreamChunk,
) {
	defer close(outCh)
	defer span.End()
	defer func() {
		if r := recover(); r != nil {
			outCh <- valueobject.StreamChunk{
				Err: fmt.Errorf("internal error (panic): %v", r),
			}
		}
	}()

	executor := s.resolveExecutor(sessionID)

	// Attach a ProgressReporter so sub-agent runners can emit lifecycle
	// events back through the same outCh used for tool events.
	reporter := subagent.NewChannelReporter(outCh)
	ctx = subagent.WithProgressReporter(ctx, reporter)

	// Phase 1: Tool execution loop with a dedicated timeout.
	toolCtx, toolCancel := context.WithTimeout(ctx, s.toolLoopTimeout)
	defer toolCancel()

	toolMessages, _, err := s.executeToolLoopWith(toolCtx, executor, messages, outCh)
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "tool execution loop failed",
			"error", err,
			"session_id", sessionID,
		)
		outCh <- valueobject.StreamChunk{Err: fmt.Errorf("tool execution loop: %w", err)}
		return
	}

	for i := range toolMessages {
		s.memory.Append(sessionID, &toolMessages[i])
	}

	// Phase 2: Stream the response, handling inline XML tool calls from
	// models (e.g. minimax) that emit tool invocations as text.
	start := time.Now()
	for i := 0; i < s.maxToolIterations; i++ {
		finalMessages := s.buildMessages(sessionID)
		req := &output.ChatRequest{
			Model:       s.model,
			Messages:    finalMessages,
			Temperature: s.temperature,
			MaxTokens:   s.maxTokens,
		}

		llmCh, err := s.client.CompleteStream(ctx, req)
		if err != nil {
			span.RecordError(err)
			slog.ErrorContext(ctx, "chat stream failed",
				"error", err,
				"session_id", sessionID,
			)
			outCh <- valueobject.StreamChunk{Err: fmt.Errorf("starting LLM stream: %w", err)}
			return
		}

		xmlTC := s.pipeStream(ctx, sessionID, llmCh, outCh)
		if xmlTC == nil {
			return
		}

		slog.InfoContext(ctx, "XML tool call detected in stream",
			"tool_name", xmlTC.Function.Name,
			"session_id", sessionID,
			"iteration", i+1,
		)

		s.emitToolEvent(outCh, valueobject.ToolEvent{
			Kind:      valueobject.ToolEventCalling,
			Iteration: i + 1,
			Total:     s.maxToolIterations,
			ToolName:  xmlTC.Function.Name,
			Elapsed:   time.Since(start),
		})

		assistantMsg := entity.NewToolCallMessage([]valueobject.ToolCall{*xmlTC})
		s.memory.Append(sessionID, &assistantMsg)

		result := s.executeToolWith(ctx, executor, *xmlTC)
		s.emitMetaToolEvents(outCh, xmlTC.Function.Name, result, time.Since(start))
		toolResult := entity.NewToolResultMessage(xmlTC.ID, result)
		s.memory.Append(sessionID, &toolResult)
	}
}

// Ask sends a single query and returns the complete response.
func (s *agentService) Ask(ctx context.Context, query string) (string, error) {
	ctx, span := s.tracer.Start(ctx, "agentService.Ask",
		trace.WithAttributes(
			attribute.String("agent.model", s.model),
			attribute.Int("query.length", len(query)),
		))
	defer span.End()

	slog.InfoContext(ctx, "ask request started",
		"model", s.model,
		"query_length", len(query),
	)

	messages := []entity.Message{
		entity.NewMessage(valueobject.RoleSystem, s.systemPrompt),
		entity.NewMessage(valueobject.RoleUser, query),
	}

	executor := s.toolExecutor
	if executor == nil && s.sessionStore != nil {
		askSessionID := fmt.Sprintf("ask-%d", time.Now().UnixNano())
		executor = s.sessionStore.GetOrCreate(askSessionID)
		defer s.sessionStore.Remove(askSessionID)
	}

	toolMessages, finalResp, err := s.executeToolLoopWith(ctx, executor, messages, nil)
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "ask tool loop failed", "error", err)
		return "", fmt.Errorf("agent query failed: %w", err)
	}

	if finalResp != nil {
		slog.InfoContext(ctx, "ask request completed",
			"model", s.model,
			"response_length", len(finalResp.Content),
		)
		return finalResp.Content, nil
	}

	messages = append(messages, toolMessages...)

	resp, err := s.client.Complete(ctx, &output.ChatRequest{
		Model:       s.model,
		Messages:    messages,
		Temperature: s.temperature,
		MaxTokens:   s.maxTokens,
	})
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "ask request failed", "error", err)
		return "", fmt.Errorf("agent query failed: %w", err)
	}

	slog.InfoContext(ctx, "ask request completed",
		"model", s.model,
		"response_length", len(resp.Content),
	)
	return resp.Content, nil
}

// resolveExecutor returns the tool.Executor to use for the given session.
// When a SessionRegistryStore is configured (JIT mode), it returns the
// session-scoped ActiveRegistry. Otherwise, it returns the default executor.
func (s *agentService) resolveExecutor(sessionID string) tool.Executor {
	if s.sessionStore != nil && sessionID != "" {
		return s.sessionStore.GetOrCreate(sessionID)
	}
	return s.toolExecutor
}

// executeToolLoopWith runs the non-streaming tool calling loop using the
// given executor and returns intermediate messages (assistant tool_calls +
// tool results) plus the final ChatResponse once no more tool calls are
// requested. When progress is non-nil, ToolEvent chunks report which tool
// is being called and elapsed time.
func (s *agentService) executeToolLoopWith(
	ctx context.Context,
	executor tool.Executor,
	messages []entity.Message,
	progress chan<- valueobject.StreamChunk,
) ([]entity.Message, *output.ChatResponse, error) {
	if executor == nil || !executor.Has() {
		return nil, nil, nil
	}

	req := &output.ChatRequest{
		Model:       s.model,
		Messages:    messages,
		Temperature: s.temperature,
		MaxTokens:   s.maxTokens,
		Tools:       executor.Definitions(),
		ToolChoice:  valueobject.ToolChoiceAuto,
	}

	start := time.Now()
	var toolMessages []entity.Message

	for i := 0; i < s.maxToolIterations; i++ {
		s.emitToolEvent(progress, valueobject.ToolEvent{
			Kind:      valueobject.ToolEventThinking,
			Iteration: i + 1,
			Total:     s.maxToolIterations,
			Elapsed:   time.Since(start),
		})

		resp, err := s.client.Complete(ctx, req)
		if err != nil {
			return nil, nil, fmt.Errorf("tool loop iteration %d: %w", i, err)
		}

		if len(resp.ToolCalls) == 0 || resp.FinishReason != "tool_calls" {
			return toolMessages, resp, nil
		}

		assistantMsg := entity.NewToolCallMessage(resp.ToolCalls)
		toolMessages = append(toolMessages, assistantMsg)
		req.Messages = append(req.Messages, assistantMsg)

		for _, tc := range resp.ToolCalls {
			s.emitToolEvent(progress, valueobject.ToolEvent{
				Kind:      valueobject.ToolEventCalling,
				Iteration: i + 1,
				Total:     s.maxToolIterations,
				ToolName:  tc.Function.Name,
				Elapsed:   time.Since(start),
			})

			result := s.executeToolWith(ctx, executor, tc)

			s.emitMetaToolEvents(progress, tc.Function.Name, result, time.Since(start))

			toolResult := entity.NewToolResultMessage(tc.ID, result)
			toolMessages = append(toolMessages, toolResult)
			req.Messages = append(req.Messages, toolResult)
		}

		// Re-fetch definitions after each iteration: a find_tools call in
		// this batch may have loaded new tool categories.
		req.Tools = executor.Definitions()
	}

	return nil, nil, fmt.Errorf("tool execution loop exceeded %d iterations", s.maxToolIterations)
}

// emitToolEvent sends a ToolEvent progress chunk if progress is non-nil.
func (s *agentService) emitToolEvent(progress chan<- valueobject.StreamChunk, event valueobject.ToolEvent) {
	if progress == nil {
		return
	}
	progress <- valueobject.StreamChunk{ToolEvent: &event}
}

// emitMetaToolEvents inspects the result of find_tools and find_skills calls,
// emitting ToolEventToolLoaded or ToolEventSkillActivated events so the UI
// can show a system message.
func (s *agentService) emitMetaToolEvents(
	progress chan<- valueobject.StreamChunk,
	toolName, result string,
	elapsed time.Duration,
) {
	if progress == nil {
		return
	}

	switch toolName {
	case "find_tools":
		var r struct {
			Category    string `json:"category"`
			Status      string `json:"status"`
			ToolsLoaded []struct {
				Name string `json:"name"`
			} `json:"tools_loaded"`
		}
		if json.Unmarshal([]byte(result), &r) == nil && r.Status == "loaded" {
			n := len(r.ToolsLoaded)
			detail := fmt.Sprintf("%d tools available", n)
			if n == 1 {
				detail = "1 tool available"
			}
			s.emitToolEvent(progress, valueobject.ToolEvent{
				Kind:     valueobject.ToolEventToolLoaded,
				ToolName: r.Category,
				Detail:   detail,
				Elapsed:  elapsed,
			})
		}

	case "find_skills":
		var r struct {
			Skill  string `json:"skill"`
			Status string `json:"status"`
		}
		if json.Unmarshal([]byte(result), &r) == nil && r.Status == "activated" {
			s.emitToolEvent(progress, valueobject.ToolEvent{
				Kind:     valueobject.ToolEventSkillActivated,
				ToolName: r.Skill,
				Detail:   "skill activated",
				Elapsed:  elapsed,
			})
		}
	}
}

func (s *agentService) executeToolWith(ctx context.Context, executor tool.Executor, tc valueobject.ToolCall) string {
	ctx, span := s.tracer.Start(ctx, "agentService.executeTool",
		trace.WithAttributes(
			attribute.String("tool.name", tc.Function.Name),
			attribute.String("tool.call_id", tc.ID),
		))
	defer span.End()

	slog.InfoContext(ctx, "executing tool",
		"tool_name", tc.Function.Name,
		"tool_call_id", tc.ID,
	)

	result, err := executor.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "tool execution failed",
			"tool_name", tc.Function.Name,
			"error", err,
		)
		errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(errJSON)
	}

	slog.InfoContext(ctx, "tool execution completed",
		"tool_name", tc.Function.Name,
		"result_length", len(result),
	)

	return result
}

func (s *agentService) buildMessages(sessionID string) []entity.Message {
	history := s.memory.History(sessionID)

	messages := make([]entity.Message, 0, len(history)+1)
	messages = append(messages, entity.NewMessage(valueobject.RoleSystem, s.systemPrompt))
	messages = append(messages, history...)
	return messages
}

// pipeStream forwards LLM stream chunks to outCh without closing it.
// Returns a parsed ToolCall if an XML tool call block was detected in the
// stream, or nil if the stream completed normally. The caller is responsible
// for executing the tool call and restarting the stream if non-nil.
func (s *agentService) pipeStream(
	ctx context.Context,
	sessionID string,
	llmCh <-chan valueobject.StreamChunk,
	outCh chan<- valueobject.StreamChunk,
) *valueobject.ToolCall {
	var fullResponse strings.Builder
	tokenCount := 0
	filter := newToolCallFilter()

	for {
		select {
		case <-ctx.Done():
			slog.WarnContext(ctx, "stream canceled",
				"session_id", sessionID,
				"tokens_received", tokenCount,
			)
			outCh <- valueobject.StreamChunk{Err: ctx.Err()}
			return nil
		case chunk, ok := <-llmCh:
			if !ok {
				s.finalizeResponse(sessionID, fullResponse.String())
				outCh <- valueobject.StreamChunk{Done: true}
				return nil
			}
			if chunk.Err != nil {
				outCh <- valueobject.StreamChunk{Err: chunk.Err}
				return nil
			}
			if chunk.Done {
				if tc := tryExtractToolCall(s, sessionID, &fullResponse, filter, llmCh); tc != nil {
					return tc
				}
				s.finalizeResponse(sessionID, fullResponse.String())
				outCh <- valueobject.StreamChunk{Done: true}
				return nil
			}
			tokenCount++

			cleaned := filter.Process(chunk.Content)
			if cleaned == "" {
				continue
			}

			if tc := tryExtractToolCall(s, sessionID, &fullResponse, filter, llmCh); tc != nil {
				return tc
			}

			fullResponse.WriteString(cleaned)
			outCh <- valueobject.StreamChunk{Content: cleaned}
		}
	}
}

// tryExtractToolCall checks whether the filter has captured an XML tool call
// block. If so, it finalizes the current response, parses the captured block,
// and drains the remaining stream. Returns the parsed ToolCall or nil.
func tryExtractToolCall(
	s *agentService,
	sessionID string,
	fullResponse *strings.Builder,
	filter *toolCallFilter,
	llmCh <-chan valueobject.StreamChunk,
) *valueobject.ToolCall {
	if !filter.HasCaptured() {
		return nil
	}
	s.finalizeResponse(sessionID, fullResponse.String())
	tc := parseXMLToolCall(filter.Captured())
	if tc != nil {
		drainStream(llmCh)
	}
	filter.ClearCaptured()
	return tc
}

func drainStream(ch <-chan valueobject.StreamChunk) {
	for {
		chunk, ok := <-ch
		if !ok || chunk.Done || chunk.Err != nil {
			return
		}
	}
}

type filterState int

const (
	filterPassthrough filterState = iota
	filterInsideToolCall
)

// toolCallFilter detects and strips XML tool call blocks that some models
// (e.g. minimax) emit as text content instead of using the API tool_calls
// mechanism. Blocks matching <minimax:tool_call>...</minimax:tool_call> are
// captured for parsing and execution.
type toolCallFilter struct {
	buf      strings.Builder
	state    filterState
	captured string
}

func newToolCallFilter() *toolCallFilter {
	return &toolCallFilter{}
}

const (
	toolCallTagOpen  = "<minimax:tool_call>"
	toolCallTagClose = "</minimax:tool_call>"
)

// HasCaptured returns true if a complete XML tool call block was captured.
func (f *toolCallFilter) HasCaptured() bool { return f.captured != "" }

// Captured returns the captured XML tool call block content.
func (f *toolCallFilter) Captured() string { return f.captured }

// ClearCaptured resets the captured content.
func (f *toolCallFilter) ClearCaptured() { f.captured = "" }

func (f *toolCallFilter) captureBlock() {
	raw := f.buf.String()
	openIdx := strings.Index(raw, toolCallTagOpen)
	closeIdx := strings.Index(raw, toolCallTagClose)
	if openIdx >= 0 && closeIdx > openIdx {
		f.captured = raw[openIdx+len(toolCallTagOpen) : closeIdx]
	}
}

// Process filters a token, returning the cleaned content to forward.
// Returns "" if the token was entirely consumed by a tool call block.
func (f *toolCallFilter) Process(token string) string {
	switch f.state {
	case filterPassthrough:
		idx := strings.Index(token, toolCallTagOpen)
		if idx < 0 {
			return token
		}
		before := token[:idx]
		f.state = filterInsideToolCall
		f.buf.Reset()
		f.buf.WriteString(token[idx:])
		if strings.Contains(f.buf.String(), toolCallTagClose) {
			f.state = filterPassthrough
			f.captureBlock()
			closeIdx := strings.Index(f.buf.String(), toolCallTagClose)
			after := strings.TrimSpace(f.buf.String()[closeIdx+len(toolCallTagClose):])
			if after != "" {
				return before + after
			}
			return before
		}
		return before
	case filterInsideToolCall:
		f.buf.WriteString(token)
		accumulated := f.buf.String()
		if strings.Contains(accumulated, toolCallTagClose) {
			f.state = filterPassthrough
			f.captureBlock()
			idx := strings.Index(accumulated, toolCallTagClose)
			return strings.TrimSpace(accumulated[idx+len(toolCallTagClose):])
		}
		return ""
	}
	return token
}

// parseXMLToolCall extracts a ToolCall from minimax XML format:
//
//	<invoke name="tool_name">
//	  <parameter name="key">value</parameter>
//	</invoke>
func parseXMLToolCall(xml string) *valueobject.ToolCall {
	invokeStart := strings.Index(xml, "<invoke")
	if invokeStart < 0 {
		return nil
	}

	nameStart := strings.Index(xml[invokeStart:], "name=\"")
	if nameStart < 0 {
		return nil
	}
	nameStart += invokeStart + len("name=\"")
	nameEnd := strings.Index(xml[nameStart:], "\"")
	if nameEnd < 0 {
		return nil
	}
	toolName := xml[nameStart : nameStart+nameEnd]

	args := make(map[string]json.RawMessage)
	remaining := xml
	for {
		paramIdx := strings.Index(remaining, "<parameter name=\"")
		if paramIdx < 0 {
			break
		}
		remaining = remaining[paramIdx+len("<parameter name=\""):]

		pNameEnd := strings.Index(remaining, "\"")
		if pNameEnd < 0 {
			break
		}
		paramName := remaining[:pNameEnd]

		valueStart := strings.Index(remaining, ">")
		if valueStart < 0 {
			break
		}
		remaining = remaining[valueStart+1:]

		valueEnd := strings.Index(remaining, "</parameter")
		if valueEnd < 0 {
			break
		}
		paramValue := strings.TrimSpace(remaining[:valueEnd])
		remaining = remaining[valueEnd:]

		if json.Valid([]byte(paramValue)) {
			args[paramName] = json.RawMessage(paramValue)
		} else {
			quoted, _ := json.Marshal(paramValue)
			args[paramName] = json.RawMessage(quoted)
		}
	}

	argsJSON, _ := json.Marshal(args)

	return &valueobject.ToolCall{
		ID:   fmt.Sprintf("xml-%s-%d", toolName, time.Now().UnixNano()),
		Type: "function",
		Function: valueobject.FunctionCall{
			Name:      toolName,
			Arguments: string(argsJSON),
		},
	}
}

func (s *agentService) finalizeResponse(sessionID, content string) {
	if content != "" {
		msg := entity.NewMessage(valueobject.RoleAssistant, content)
		s.memory.Append(sessionID, &msg)
		s.memory.Trim(sessionID, s.historyLimit)
	}
}
