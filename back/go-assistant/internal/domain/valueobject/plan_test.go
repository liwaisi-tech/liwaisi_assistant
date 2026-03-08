package valueobject

import (
	"testing"
	"time"
)

func TestPlanStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status PlanStatus
		want   bool
	}{
		{PlanStatusPending, false},
		{PlanStatusRunning, false},
		{PlanStatusCompleted, true},
		{PlanStatusFailed, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMicroTaskResult_IsSuccess(t *testing.T) {
	tests := []struct {
		name   string
		result MicroTaskResult
		want   bool
	}{
		{"completed no error", MicroTaskResult{Status: PlanStatusCompleted}, true},
		{"failed status", MicroTaskResult{Status: PlanStatusFailed}, false},
		{"pending status", MicroTaskResult{Status: PlanStatusPending}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsSuccess(); got != tt.want {
				t.Errorf("IsSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanResult_IsSuccess(t *testing.T) {
	ok := PlanResult{Status: PlanStatusCompleted}
	if !ok.IsSuccess() {
		t.Error("IsSuccess() should be true for Completed")
	}
	fail := PlanResult{Status: PlanStatusFailed}
	if fail.IsSuccess() {
		t.Error("IsSuccess() should be false for Failed")
	}
}

func TestPlanResult_FailedTasks(t *testing.T) {
	pr := PlanResult{
		Status: PlanStatusFailed,
		Results: []MicroTaskResult{
			{TaskID: "A", Status: PlanStatusCompleted},
			{TaskID: "B", Status: PlanStatusFailed, Elapsed: 1 * time.Second},
			{TaskID: "C", Status: PlanStatusCompleted},
		},
	}

	failed := pr.FailedTasks()
	if len(failed) != 1 || failed[0].TaskID != "B" {
		t.Errorf("FailedTasks() = %v, want [B]", failed)
	}
}
