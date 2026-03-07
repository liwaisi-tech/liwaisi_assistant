package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// LLMClientFactory resolves an LLM client and model name for a given
// ModelTier. This allows the Runner to select different models based on
// the subagent's tier without hardcoding model strings.
type LLMClientFactory func(tier valueobject.ModelTier) (client output.LLMClient, model string)

// RunnerOption configures optional Runner behavior.
type RunnerOption func(*Runner)

// WithRunnerRecovery overrides the default RecoveryConfig used by the
// Runner for every subagent execution.
func WithRunnerRecovery(cfg RecoveryConfig) RunnerOption {
	return func(r *Runner) {
		r.recoveryCfg = cfg
	}
}

// WithApproval overrides the default ApprovalConfig used by the Runner.
// When a non-auto-approve gate is set, the runner presents successful
// results for review and supports revision cycles on rejection.
func WithApproval(cfg ApprovalConfig) RunnerOption {
	return func(r *Runner) {
		r.approvalCfg = cfg
	}
}

// Runner executes a subagent's autonomous LLM loop. It creates an
// isolated child context, runs tool calls in a loop, and returns the
// subagent's result. Runner is safe for concurrent use.
type Runner struct {
	clientFactory LLMClientFactory
	memFactory    *memory.SubAgentMemoryFactory
	recoveryCfg   RecoveryConfig
	approvalCfg   ApprovalConfig
}

// NewRunner creates a Runner backed by the given client factory and
// memory factory. Options can override the default recovery config.
func NewRunner(factory LLMClientFactory, memFactory *memory.SubAgentMemoryFactory, opts ...RunnerOption) *Runner {
	r := &Runner{
		clientFactory: factory,
		memFactory:    memFactory,
		recoveryCfg:   DefaultRecoveryConfig(),
		approvalCfg:   DefaultApprovalConfig(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run executes a subagent to completion. The execution is wrapped with
// WithRecovery for timeout enforcement and cascading cancellation. Each
// LLM call is wrapped with RetryLLMCall for transient error resilience.
// When an ApprovalGate is configured, successful results are presented
// for review. Rejected results trigger a revision cycle up to
// MaxRevisions times.
func (r *Runner) Run(
	ctx context.Context,
	spec *entity.SubAgentSpec,
	executor tool.Executor,
	task string,
	parentSessionID string,
) valueobject.SubAgentResult {
	start := time.Now()
	recCfg := r.effectiveRecovery(spec)
	currentTask := task
	var cumulativeUsage valueobject.SubAgentUsage

	for revision := 0; revision <= r.approvalCfg.MaxRevisions; revision++ {
		result := WithRecovery(ctx, recCfg, func(execCtx context.Context) valueobject.SubAgentResult {
			return r.runLoop(execCtx, recCfg, spec, executor, currentTask, parentSessionID, start)
		})

		cumulativeUsage.PromptTokens += result.Usage.PromptTokens
		cumulativeUsage.CompletionTokens += result.Usage.CompletionTokens
		cumulativeUsage.TotalTokens += result.Usage.TotalTokens

		if !result.IsSuccess() {
			result.Usage = cumulativeUsage
			return result
		}

		decision, err := r.approvalCfg.Gate.Review(ctx, &result)
		if err != nil {
			return r.failedResult(spec.Name, start, cumulativeUsage, fmt.Errorf("approval gate: %w", err))
		}
		if decision.Approved {
			result.Usage = cumulativeUsage
			result.Elapsed = time.Since(start)
			return result
		}
		if decision.Canceled {
			return r.canceledResult(spec.Name, start, cumulativeUsage)
		}

		currentTask = formatRevisionTask(task, result.Output, decision.Feedback)
	}

	return r.failedResult(spec.Name, start, cumulativeUsage,
		fmt.Errorf("max revisions (%d) exceeded without approval", r.approvalCfg.MaxRevisions))
}

// formatRevisionTask builds a revision prompt that includes the original
// task, the previous output, and the reviewer's feedback.
func formatRevisionTask(originalTask, output, feedback string) string {
	return fmt.Sprintf(
		"Original task:\n%s\n\nYour previous output:\n%s\n\nReviewer feedback:\n%s\n\nPlease revise your output based on the feedback above.",
		originalTask, output, feedback,
	)
}

// effectiveRecovery merges the runner's default config with the spec's
// timeout override so each subagent can declare its own timeout.
func (r *Runner) effectiveRecovery(spec *entity.SubAgentSpec) RecoveryConfig {
	cfg := r.recoveryCfg
	cfg.Timeout = spec.EffectiveTimeout()
	return cfg
}

// runLoop is the inner LLM tool loop, executed inside WithRecovery.
func (r *Runner) runLoop(
	execCtx context.Context,
	cfg RecoveryConfig,
	spec *entity.SubAgentSpec,
	executor tool.Executor,
	task string,
	parentSessionID string,
	start time.Time,
) (result valueobject.SubAgentResult) {
	reporter := ProgressReporterFrom(execCtx)

	childID := r.memFactory.CreateChildContext(parentSessionID, spec)
	defer r.memFactory.CleanupChild(childID)

	maxTurns := spec.EffectiveMaxTurns()

	defer func() {
		reportProgress(reporter, valueobject.ToolEvent{
			Kind:     valueobject.ToolEventSubAgentCompleted,
			ToolName: spec.Name,
			Detail:   string(result.Status),
			Elapsed:  time.Since(start),
		})
	}()

	reportProgress(reporter, valueobject.ToolEvent{
		Kind:     valueobject.ToolEventSubAgentStarted,
		ToolName: spec.Name,
		Total:    maxTurns,
		Elapsed:  time.Since(start),
	})

	client, model := r.clientFactory(spec.ModelTier)

	taskMsg := entity.NewMessage(valueobject.RoleUser, task)
	r.memFactory.Append(childID, &taskMsg)

	var totalUsage valueobject.SubAgentUsage
	var lastContent string
	var lastHadToolCalls bool

	for turn := 0; turn < maxTurns; turn++ {
		if err := execCtx.Err(); err != nil {
			return r.canceledResult(spec.Name, start, totalUsage)
		}

		reportProgress(reporter, valueobject.ToolEvent{
			Kind:      valueobject.ToolEventSubAgentThinking,
			ToolName:  spec.Name,
			Iteration: turn + 1,
			Total:     maxTurns,
			Elapsed:   time.Since(start),
		})

		messages := r.memFactory.ChildHistory(childID)

		var tools []valueobject.ToolDefinition
		if executor != nil && executor.Has() {
			tools = executor.Definitions()
		}

		req := &output.ChatRequest{
			Model:    model,
			Messages: messages,
			Tools:    tools,
		}
		if len(tools) > 0 {
			req.ToolChoice = valueobject.ToolChoiceAuto
		}

		resp, err := RetryLLMCall(execCtx, cfg, func(retryCtx context.Context) (*output.ChatResponse, error) {
			return client.Complete(retryCtx, req)
		})
		if err != nil {
			if execCtx.Err() != nil {
				return r.canceledResult(spec.Name, start, totalUsage)
			}
			return r.failedResult(spec.Name, start, totalUsage, err)
		}

		totalUsage.PromptTokens += resp.PromptTokens
		totalUsage.CompletionTokens += resp.CompletionTokens
		totalUsage.TotalTokens += resp.TotalTokens

		if len(resp.ToolCalls) == 0 {
			lastContent = resp.Content
			lastHadToolCalls = false
			break
		}

		lastHadToolCalls = true
		assistantMsg := entity.NewToolCallMessage(resp.ToolCalls)
		r.memFactory.Append(childID, &assistantMsg)

		for _, tc := range resp.ToolCalls {
			reportProgress(reporter, valueobject.ToolEvent{
				Kind:      valueobject.ToolEventSubAgentToolCall,
				ToolName:  spec.Name,
				Detail:    tc.Function.Name,
				Iteration: turn + 1,
				Total:     maxTurns,
				Elapsed:   time.Since(start),
			})

			toolResult := r.executeTool(execCtx, executor, tc)
			toolResultMsg := entity.NewToolResultMessage(tc.ID, toolResult)
			r.memFactory.Append(childID, &toolResultMsg)
		}

		lastContent = resp.Content

		if turn == maxTurns-1 {
			slog.WarnContext(execCtx, "subagent reached max turns",
				"agent", spec.Name,
				"max_turns", maxTurns,
			)
		}
	}

	status := valueobject.SubAgentStatusCompleted
	if lastHadToolCalls {
		status = valueobject.SubAgentStatusMaxTurns
	}

	return valueobject.SubAgentResult{
		AgentName: spec.Name,
		Output:    lastContent,
		Status:    status,
		Usage:     totalUsage,
		Elapsed:   time.Since(start),
	}
}

// reportProgress emits an event via the reporter if it is non-nil.
func reportProgress(reporter ProgressReporter, event valueobject.ToolEvent) {
	if reporter != nil {
		reporter.Report(event)
	}
}

func (r *Runner) executeTool(ctx context.Context, executor tool.Executor, tc valueobject.ToolCall) string {
	slog.Info("subagent executing tool",
		"tool_name", tc.Function.Name,
		"tool_call_id", tc.ID,
	)

	result, err := executor.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
	if err != nil {
		slog.Error("subagent tool execution failed",
			"tool_name", tc.Function.Name,
			"error", err,
		)
		errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(errJSON)
	}
	return result
}

func (r *Runner) canceledResult(agentName string, start time.Time, usage valueobject.SubAgentUsage) valueobject.SubAgentResult {
	return valueobject.SubAgentResult{
		AgentName: agentName,
		Err:       fmt.Errorf("subagent %q canceled", agentName),
		Status:    valueobject.SubAgentStatusCanceled,
		Usage:     usage,
		Elapsed:   time.Since(start),
	}
}

func (r *Runner) failedResult(agentName string, start time.Time, usage valueobject.SubAgentUsage, err error) valueobject.SubAgentResult {
	return valueobject.SubAgentResult{
		AgentName: agentName,
		Err:       fmt.Errorf("subagent %q LLM error: %w", agentName, err),
		Status:    valueobject.SubAgentStatusFailed,
		Usage:     usage,
		Elapsed:   time.Since(start),
	}
}
