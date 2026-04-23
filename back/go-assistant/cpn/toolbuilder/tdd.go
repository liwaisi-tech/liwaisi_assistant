package toolbuilder

// tdd.go — t-tdd-loop real implementation.
//
// The loop runs per subtask inside each total:
//
//	for iter := 1..MaxTDDIterations:
//	    1. TDDRunner.WriteFailingTest  → materialise a failing test file
//	    2. TDDRunner.RunTest            → expect FAIL (red)
//	    3. TDDRunner.WriteImplementation→ materialise the implementation
//	    4. TDDRunner.RunTest            → expect PASS (green)
//	    5. TDDRunner.Coverage           → must be ≥ CoverageFloor
//	    break on pass+coverage; else loop
//
// The handler is deterministic Go; the *work* (LLM calls, bash execution)
// lives behind the TDDRunner interface so this package stays side-effect
// free and fully unit-testable with a fake.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// TDDRunner is the collaborator that owns LLM calls and bash execution for
// the TDD loop. Implementations live in adapters; the handler only cares
// about the contract.
type TDDRunner interface {
	// WriteFailingTest asks the LLM for a failing test for the subtask and
	// writes it into the workspace. Returns an optional note for audit.
	WriteFailingTest(ctx context.Context, workspace string, subtask Subtask) (note string, err error)

	// WriteImplementation asks the LLM for the implementation that makes the
	// named test pass, and writes it into the workspace.
	WriteImplementation(ctx context.Context, workspace string, subtask Subtask) (note string, err error)

	// RunTest runs the single test (`go test -run <TestName>`). Returns the
	// pass/fail outcome, coverage %, and a short log line for audit.
	RunTest(ctx context.Context, workspace, testName string) (passed bool, coverage float64, log string, err error)
}

// NoopTDDRunner satisfies TDDRunner without touching disk or network —
// useful in unit tests and as a safe default when no real runner is wired.
// It reports every subtask as "skipped" so the pipeline still drains.
type NoopTDDRunner struct{}

func (NoopTDDRunner) WriteFailingTest(context.Context, string, Subtask) (string, error) {
	return "noop: no test written", nil
}
func (NoopTDDRunner) WriteImplementation(context.Context, string, Subtask) (string, error) {
	return "noop: no implementation written", nil
}
func (NoopTDDRunner) RunTest(context.Context, string, string) (bool, float64, string, error) {
	return false, 0, "noop: did not run", nil
}

// tddLoopHandler returns the cpnToolHandler wired into t-tdd-loop.
// runner may be nil — the handler falls back to NoopTDDRunner so the
// topology is always firable end-to-end.
func tddLoopHandler(runner TDDRunner) cpnToolHandler {
	if runner == nil {
		runner = NoopTDDRunner{}
	}
	return func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, errors.New("t-tdd-loop: no input token")
		}
		var subs SubtasksBatch
		if err := decodeAs(consumed[0].Payload, &subs); err != nil {
			return nil, fmt.Errorf("t-tdd-loop: decode subtasks batch: %w", err)
		}

		out := TestedArtifact{
			SpecName:      subs.SpecName,
			WorkspacePath: subs.WorkspacePath,
			PerTotal:      make([]TotalTested, 0, len(subs.ByTotal)),
		}

		var sumCoverage float64
		var coverageSamples int

		for _, group := range subs.ByTotal {
			result := runTotal(ctx, runner, subs.WorkspacePath, group)
			out.PerTotal = append(out.PerTotal, result)
			if result.Coverage > 0 {
				sumCoverage += result.Coverage
				coverageSamples++
			}
		}
		if coverageSamples > 0 {
			out.CoveragePct = sumCoverage / float64(coverageSamples)
		}

		payload, err := json.Marshal(out)
		if err != nil {
			return nil, fmt.Errorf("t-tdd-loop: marshal: %w", err)
		}
		return map[string]cpn.Token{
			PlaceTested: {Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: string(payload)},
		}, nil
	}
}

// runTotal executes the TDD loop for every subtask in a total and folds
// the results into a single TotalTested. A total is "passed" iff every
// subtask ends green with coverage ≥ CoverageFloor.
func runTotal(ctx context.Context, runner TDDRunner, workspace string, group TotalSubs) TotalTested {
	out := TotalTested{TotalID: group.TotalID, Passed: true}
	if len(group.Subtasks) == 0 {
		out.Passed = false
		out.Notes = "no subtasks supplied"
		return out
	}
	var maxCov float64
	for _, st := range group.Subtasks {
		passed, iters, cov, note := runSubtask(ctx, runner, workspace, st)
		out.Iterations += iters
		if cov > maxCov {
			maxCov = cov
		}
		if !passed {
			out.Passed = false
			if out.Notes == "" {
				out.Notes = fmt.Sprintf("subtask %s failed: %s", st.ID, note)
			}
		}
	}
	out.Coverage = maxCov
	if out.Passed && maxCov < CoverageFloor {
		out.Passed = false
		out.Notes = fmt.Sprintf("coverage %.1f%% below floor %.1f%%", maxCov, CoverageFloor)
	}
	return out
}

// runSubtask iterates the red-green-refactor loop for a single subtask.
// Returns (passed, iterations, bestCoverage, note).
func runSubtask(ctx context.Context, runner TDDRunner, workspace string, st Subtask) (bool, int, float64, string) {
	var bestCov float64
	for iter := 1; iter <= MaxTDDIterations; iter++ {
		if _, err := runner.WriteFailingTest(ctx, workspace, st); err != nil {
			return false, iter, bestCov, "write test: " + err.Error()
		}
		// Red step: test should currently fail.
		passed, _, _, err := runner.RunTest(ctx, workspace, st.TestName)
		if err != nil {
			return false, iter, bestCov, "red run: " + err.Error()
		}
		if passed {
			// Non-failing "failing" test — LLM didn't produce a real red test.
			// Move on to implementation anyway; this is a soft warning.
		}

		if _, err := runner.WriteImplementation(ctx, workspace, st); err != nil {
			return false, iter, bestCov, "write impl: " + err.Error()
		}
		greenPassed, cov, _, err := runner.RunTest(ctx, workspace, st.TestName)
		if err != nil {
			return false, iter, bestCov, "green run: " + err.Error()
		}
		if cov > bestCov {
			bestCov = cov
		}
		if greenPassed && cov >= CoverageFloor {
			return true, iter, bestCov, ""
		}
	}
	return false, MaxTDDIterations, bestCov, "exhausted iterations"
}
