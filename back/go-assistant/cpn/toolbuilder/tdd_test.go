package toolbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// fakeTDDRunner is a deterministic test double. Callers program it by
// setting per-subtask scripts keyed by subtask ID.
type fakeTDDRunner struct {
	// scripts maps subtask ID → sequence of RunTest outcomes. Each
	// iteration pops two outcomes (red then green). Defaults: red=fail,
	// green=pass with covPerIter coverage.
	scripts map[string][]runOutcome

	writeTestErr map[string]error
	writeImplErr map[string]error
	covPerIter   float64
	testCalls    map[string]int
}

type runOutcome struct {
	passed bool
	cov    float64
	err    error
}

func newFakeRunner(covPerIter float64) *fakeTDDRunner {
	return &fakeTDDRunner{
		scripts:      map[string][]runOutcome{},
		writeTestErr: map[string]error{},
		writeImplErr: map[string]error{},
		covPerIter:   covPerIter,
		testCalls:    map[string]int{},
	}
}

func (f *fakeTDDRunner) WriteFailingTest(_ context.Context, _ string, st Subtask) (string, error) {
	if err, ok := f.writeTestErr[st.ID]; ok {
		return "", err
	}
	return "ok", nil
}

func (f *fakeTDDRunner) WriteImplementation(_ context.Context, _ string, st Subtask) (string, error) {
	if err, ok := f.writeImplErr[st.ID]; ok {
		return "", err
	}
	return "ok", nil
}

func (f *fakeTDDRunner) RunTest(_ context.Context, _ string, testName string) (bool, float64, string, error) {
	key := testName
	f.testCalls[key]++
	callIdx := f.testCalls[key] - 1 // 0-based
	if seq, ok := f.scripts[key]; ok && callIdx < len(seq) {
		o := seq[callIdx]
		return o.passed, o.cov, "", o.err
	}
	// Default: even call = red (fail), odd call = green (pass + cov).
	if callIdx%2 == 0 {
		return false, 0, "", nil
	}
	return true, f.covPerIter, "", nil
}

func TestNoopTDDRunner_Implements(t *testing.T) {
	var r TDDRunner = NoopTDDRunner{}
	note, err := r.WriteFailingTest(context.Background(), "/w", Subtask{ID: "s1"})
	if err != nil || note == "" {
		t.Fatalf("noop WriteFailingTest: note=%q err=%v", note, err)
	}
	_, err = r.WriteImplementation(context.Background(), "/w", Subtask{})
	if err != nil {
		t.Fatal(err)
	}
	passed, cov, _, err := r.RunTest(context.Background(), "/w", "X")
	if err != nil || passed || cov != 0 {
		t.Fatalf("noop RunTest: passed=%v cov=%v err=%v", passed, cov, err)
	}
}

func TestTDDLoopHandler_NoInput(t *testing.T) {
	h := tddLoopHandler(nil) // nil → falls back to noop
	if _, err := h(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestTDDLoopHandler_BadPayload(t *testing.T) {
	h := tddLoopHandler(NoopTDDRunner{})
	tok := cpn.Token{Payload: "not-json"}
	if _, err := h(context.Background(), []cpn.Token{tok}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestTDDLoopHandler_HappyPath(t *testing.T) {
	runner := newFakeRunner(90.0)
	batch := SubtasksBatch{
		SpecName:      "calc",
		WorkspacePath: "/workspace/calc",
		ByTotal: []TotalSubs{
			{TotalID: "T1", Subtasks: []Subtask{{ID: "T1.s1", TestName: "TestAdd"}}},
		},
	}
	payload, _ := json.Marshal(batch)
	h := tddLoopHandler(runner)
	out, err := h(context.Background(), []cpn.Token{{Payload: string(payload)}})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	tok, ok := out[PlaceTested]
	if !ok {
		t.Fatal("missing PlaceTested")
	}
	var tested TestedArtifact
	if err := json.Unmarshal([]byte(tok.Payload.(string)), &tested); err != nil {
		t.Fatal(err)
	}
	if tested.SpecName != "calc" || tested.WorkspacePath != "/workspace/calc" {
		t.Fatalf("propagation: %+v", tested)
	}
	if len(tested.PerTotal) != 1 || !tested.PerTotal[0].Passed {
		t.Fatalf("expected passed total, got %+v", tested.PerTotal)
	}
	if tested.PerTotal[0].Coverage < CoverageFloor {
		t.Fatalf("coverage below floor: %f", tested.PerTotal[0].Coverage)
	}
	if tested.CoveragePct == 0 {
		t.Fatal("expected averaged coverage > 0")
	}
}

func TestTDDLoopHandler_CoverageFloorEnforced(t *testing.T) {
	runner := newFakeRunner(50.0) // below CoverageFloor=85
	batch := SubtasksBatch{
		ByTotal: []TotalSubs{
			{TotalID: "T1", Subtasks: []Subtask{{ID: "T1.s1", TestName: "TestLow"}}},
		},
	}
	payload, _ := json.Marshal(batch)
	h := tddLoopHandler(runner)
	out, _ := h(context.Background(), []cpn.Token{{Payload: string(payload)}})
	var tested TestedArtifact
	_ = json.Unmarshal([]byte(out[PlaceTested].Payload.(string)), &tested)
	if tested.PerTotal[0].Passed {
		t.Fatal("expected failure due to coverage floor")
	}
	if tested.PerTotal[0].Iterations != MaxTDDIterations {
		t.Fatalf("expected %d iterations, got %d", MaxTDDIterations, tested.PerTotal[0].Iterations)
	}
}

func TestTDDLoopHandler_EmptySubtasksFails(t *testing.T) {
	batch := SubtasksBatch{ByTotal: []TotalSubs{{TotalID: "T1"}}}
	payload, _ := json.Marshal(batch)
	h := tddLoopHandler(newFakeRunner(90))
	out, _ := h(context.Background(), []cpn.Token{{Payload: string(payload)}})
	var tested TestedArtifact
	_ = json.Unmarshal([]byte(out[PlaceTested].Payload.(string)), &tested)
	if tested.PerTotal[0].Passed {
		t.Fatal("total with no subtasks must not be reported passed")
	}
}

func TestTDDLoopHandler_WriteTestError(t *testing.T) {
	runner := newFakeRunner(90)
	runner.writeTestErr["T1.s1"] = errors.New("disk full")
	batch := SubtasksBatch{
		ByTotal: []TotalSubs{
			{TotalID: "T1", Subtasks: []Subtask{{ID: "T1.s1", TestName: "TestX"}}},
		},
	}
	payload, _ := json.Marshal(batch)
	h := tddLoopHandler(runner)
	out, _ := h(context.Background(), []cpn.Token{{Payload: string(payload)}})
	var tested TestedArtifact
	_ = json.Unmarshal([]byte(out[PlaceTested].Payload.(string)), &tested)
	if tested.PerTotal[0].Passed {
		t.Fatal("expected failure on write-test error")
	}
}

func TestTDDLoopHandler_WriteImplError(t *testing.T) {
	runner := newFakeRunner(90)
	runner.writeImplErr["T1.s1"] = errors.New("llm unreachable")
	batch := SubtasksBatch{
		ByTotal: []TotalSubs{
			{TotalID: "T1", Subtasks: []Subtask{{ID: "T1.s1", TestName: "TestX"}}},
		},
	}
	payload, _ := json.Marshal(batch)
	h := tddLoopHandler(runner)
	out, _ := h(context.Background(), []cpn.Token{{Payload: string(payload)}})
	var tested TestedArtifact
	_ = json.Unmarshal([]byte(out[PlaceTested].Payload.(string)), &tested)
	if tested.PerTotal[0].Passed {
		t.Fatal("expected failure on write-impl error")
	}
}

func TestTDDLoopHandler_RunTestError(t *testing.T) {
	runner := newFakeRunner(90)
	runner.scripts["TestBoom"] = []runOutcome{{err: errors.New("bash crashed")}}
	batch := SubtasksBatch{
		ByTotal: []TotalSubs{
			{TotalID: "T1", Subtasks: []Subtask{{ID: "T1.s1", TestName: "TestBoom"}}},
		},
	}
	payload, _ := json.Marshal(batch)
	h := tddLoopHandler(runner)
	out, _ := h(context.Background(), []cpn.Token{{Payload: string(payload)}})
	var tested TestedArtifact
	_ = json.Unmarshal([]byte(out[PlaceTested].Payload.(string)), &tested)
	if tested.PerTotal[0].Passed {
		t.Fatal("expected failure on run-test error")
	}
}

func TestTDDLoopHandler_RetryThenSucceed(t *testing.T) {
	runner := newFakeRunner(90)
	// iter 1: red=fail ok, green=fail (cov low) ; iter 2: red=fail, green=pass w/ cov.
	runner.scripts["TestFlaky"] = []runOutcome{
		{passed: false, cov: 0},    // red1
		{passed: false, cov: 40},   // green1 — low
		{passed: false, cov: 0},    // red2
		{passed: true, cov: 92},    // green2 — passes
	}
	batch := SubtasksBatch{
		ByTotal: []TotalSubs{
			{TotalID: "T1", Subtasks: []Subtask{{ID: "T1.s1", TestName: "TestFlaky"}}},
		},
	}
	payload, _ := json.Marshal(batch)
	h := tddLoopHandler(runner)
	out, _ := h(context.Background(), []cpn.Token{{Payload: string(payload)}})
	var tested TestedArtifact
	_ = json.Unmarshal([]byte(out[PlaceTested].Payload.(string)), &tested)
	if !tested.PerTotal[0].Passed {
		t.Fatalf("expected pass after retry, got %+v", tested.PerTotal[0])
	}
	if tested.PerTotal[0].Iterations != 2 {
		t.Fatalf("expected 2 iterations, got %d", tested.PerTotal[0].Iterations)
	}
}

func TestTDDLoopHandler_MultipleTotals(t *testing.T) {
	runner := newFakeRunner(90)
	batch := SubtasksBatch{
		SpecName: "multi",
		ByTotal: []TotalSubs{
			{TotalID: "T1", Subtasks: []Subtask{{ID: "T1.s1", TestName: "TestA"}}},
			{TotalID: "T2", Subtasks: []Subtask{{ID: "T2.s1", TestName: "TestB"}}},
		},
	}
	payload, _ := json.Marshal(batch)
	h := tddLoopHandler(runner)
	out, _ := h(context.Background(), []cpn.Token{{Payload: string(payload)}})
	var tested TestedArtifact
	_ = json.Unmarshal([]byte(out[PlaceTested].Payload.(string)), &tested)
	if len(tested.PerTotal) != 2 {
		t.Fatalf("expected 2 totals in artifact, got %d", len(tested.PerTotal))
	}
	for _, r := range tested.PerTotal {
		if !r.Passed {
			t.Fatalf("total %s expected passed, got %+v", r.TotalID, r)
		}
	}
}
