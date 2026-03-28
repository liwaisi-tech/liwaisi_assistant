package cpn

import (
	"sync"
	"testing"
	"time"
)

func TestExecutionTracker_RecordTransitionFired(t *testing.T) {
	tests := []struct {
		name            string
		firings         []NodeKind
		wantTransitions int
		wantLLM         int
	}{
		{
			name:            "tool transitions only",
			firings:         []NodeKind{NodeKindTool, NodeKindTool, NodeKindTool},
			wantTransitions: 3,
			wantLLM:         0,
		},
		{
			name:            "LLM transitions only",
			firings:         []NodeKind{NodeKindLLM, NodeKindLLM},
			wantTransitions: 2,
			wantLLM:         2,
		},
		{
			name:            "mixed transitions",
			firings:         []NodeKind{NodeKindTool, NodeKindLLM, NodeKindValidate, NodeKindLLM, NodeKindSubNet},
			wantTransitions: 5,
			wantLLM:         2,
		},
		{
			name:            "no transitions",
			firings:         nil,
			wantTransitions: 0,
			wantLLM:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newExecutionTracker("cpn-1", "analyst", 1, "sess-1")

			for _, kind := range tt.firings {
				tracker.RecordTransitionFired(kind)
			}

			if got := int(tracker.transitionsFired.Load()); got != tt.wantTransitions {
				t.Errorf("transitionsFired = %d, want %d", got, tt.wantTransitions)
			}
			if got := int(tracker.llmCallCount.Load()); got != tt.wantLLM {
				t.Errorf("llmCallCount = %d, want %d", got, tt.wantLLM)
			}
		})
	}
}

func TestExecutionTracker_RecordTokensProduced(t *testing.T) {
	tracker := newExecutionTracker("cpn-1", "analyst", 1, "sess-1")

	tracker.RecordTokensProduced(3)
	tracker.RecordTokensProduced(5)
	tracker.RecordTokensProduced(2)

	if got := int(tracker.tokensProduced.Load()); got != 10 {
		t.Errorf("tokensProduced = %d, want 10", got)
	}
}

func TestExecutionTracker_Finalize(t *testing.T) {
	tracker := newExecutionTracker("cpn-1", "analyst", 2, "sess-42")
	// Allow a small duration to elapse.
	time.Sleep(time.Millisecond)

	tracker.RecordTransitionFired(NodeKindTool)
	tracker.RecordTransitionFired(NodeKindLLM)
	tracker.RecordTransitionFired(NodeKindLLM)
	tracker.RecordTokensProduced(7)

	rec := tracker.Finalize(true, 0.05)

	if rec.CPNID != "cpn-1" {
		t.Errorf("CPNID = %q, want %q", rec.CPNID, "cpn-1")
	}
	if rec.CPNRole != "analyst" {
		t.Errorf("CPNRole = %q, want %q", rec.CPNRole, "analyst")
	}
	if rec.CPNDepth != 2 {
		t.Errorf("CPNDepth = %d, want 2", rec.CPNDepth)
	}
	if rec.SessionID != "sess-42" {
		t.Errorf("SessionID = %q, want %q", rec.SessionID, "sess-42")
	}
	if rec.TransitionsFired != 3 {
		t.Errorf("TransitionsFired = %d, want 3", rec.TransitionsFired)
	}
	if rec.LLMCallCount != 2 {
		t.Errorf("LLMCallCount = %d, want 2", rec.LLMCallCount)
	}
	if rec.TotalCostUSD != 0.05 {
		t.Errorf("TotalCostUSD = %f, want 0.05", rec.TotalCostUSD)
	}
	if rec.TokensProduced != 7 {
		t.Errorf("TokensProduced = %d, want 7", rec.TokensProduced)
	}
	if rec.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", rec.Duration)
	}
	if !rec.Success {
		t.Error("Success = false, want true")
	}
	if rec.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
	if rec.CompletedAt.IsZero() {
		t.Error("CompletedAt is zero")
	}
	if !rec.CompletedAt.After(rec.StartedAt) {
		t.Errorf("CompletedAt (%v) should be after StartedAt (%v)", rec.CompletedAt, rec.StartedAt)
	}
}

func TestExecutionTracker_Finalize_Failed(t *testing.T) {
	tracker := newExecutionTracker("cpn-2", "worker", 3, "sess-99")
	rec := tracker.Finalize(false, 0.0)

	if rec.Success {
		t.Error("Success = true, want false")
	}
	if rec.TransitionsFired != 0 {
		t.Errorf("TransitionsFired = %d, want 0", rec.TransitionsFired)
	}
}

func TestMetricsRecorder_Append(t *testing.T) {
	mr := NewMetricsRecorder()

	if mr.Len() != 0 {
		t.Errorf("Len = %d, want 0", mr.Len())
	}

	mr.Append(&ExecutionRecord{CPNID: "cpn-1", Success: true})
	mr.Append(&ExecutionRecord{CPNID: "cpn-2", Success: false})
	mr.Append(&ExecutionRecord{CPNID: "cpn-3", Success: true})

	if mr.Len() != 3 {
		t.Errorf("Len = %d, want 3", mr.Len())
	}

	records := mr.Records()
	if len(records) != 3 {
		t.Fatalf("Records() len = %d, want 3", len(records))
	}
	if records[0].CPNID != "cpn-1" {
		t.Errorf("records[0].CPNID = %q, want %q", records[0].CPNID, "cpn-1")
	}
	if records[2].CPNID != "cpn-3" {
		t.Errorf("records[2].CPNID = %q, want %q", records[2].CPNID, "cpn-3")
	}
}

func TestMetricsRecorder_Records_ReturnsCopy(t *testing.T) {
	mr := NewMetricsRecorder()
	mr.Append(&ExecutionRecord{CPNID: "cpn-1", LLMCallCount: 5})

	records := mr.Records()
	records[0].LLMCallCount = 999 // mutate the copy

	// Internal state should be unchanged.
	original := mr.Records()
	if original[0].LLMCallCount != 5 {
		t.Errorf("internal LLMCallCount = %d, want 5 (copy-on-read violated)", original[0].LLMCallCount)
	}
}

func TestMetricsRecorder_Records_Empty(t *testing.T) {
	mr := NewMetricsRecorder()
	records := mr.Records()
	if records != nil {
		t.Errorf("Records() = %v, want nil for empty recorder", records)
	}
}

func TestMetricsRecorder_ConcurrentAppend(t *testing.T) {
	mr := NewMetricsRecorder()
	const goroutines = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func() {
			defer wg.Done()
			mr.Append(&ExecutionRecord{
				CPNID:            "cpn-concurrent",
				TransitionsFired: i,
			})
		}()
	}

	wg.Wait()

	if mr.Len() != goroutines {
		t.Errorf("Len = %d, want %d after concurrent append", mr.Len(), goroutines)
	}
}
