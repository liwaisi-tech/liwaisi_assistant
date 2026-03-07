package valueobject

import (
	"errors"
	"testing"
	"time"
)

func TestModelTier_String(t *testing.T) {
	tests := []struct {
		name string
		tier ModelTier
		want string
	}{
		{"fast tier", ModelTierFast, "fast"},
		{"balanced tier", ModelTierBalanced, "balanced"},
		{"capable tier", ModelTierCapable, "capable"},
		{"empty tier", ModelTier(""), ""},
		{"unknown tier", ModelTier("unknown"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tier.String(); got != tt.want {
				t.Errorf("ModelTier.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModelTier_IsValid(t *testing.T) {
	tests := []struct {
		name string
		tier ModelTier
		want bool
	}{
		{"fast is valid", ModelTierFast, true},
		{"balanced is valid", ModelTierBalanced, true},
		{"capable is valid", ModelTierCapable, true},
		{"empty is invalid", ModelTier(""), false},
		{"unknown is invalid", ModelTier("turbo"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tier.IsValid(); got != tt.want {
				t.Errorf("ModelTier.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelTier_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tier    ModelTier
		wantErr bool
	}{
		{"fast no error", ModelTierFast, false},
		{"balanced no error", ModelTierBalanced, false},
		{"capable no error", ModelTierCapable, false},
		{"empty returns error", ModelTier(""), true},
		{"unknown returns error", ModelTier("turbo"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tier.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ModelTier.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSubAgentStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status SubAgentStatus
		want   string
	}{
		{"pending", SubAgentStatusPending, "pending"},
		{"running", SubAgentStatusRunning, "running"},
		{"completed", SubAgentStatusCompleted, "completed"},
		{"failed", SubAgentStatusFailed, "failed"},
		{"canceled", SubAgentStatusCanceled, "canceled"},
		{"max_turns_exhausted", SubAgentStatusMaxTurns, "max_turns_exhausted"},
		{"empty", SubAgentStatus(""), ""},
		{"unknown", SubAgentStatus("paused"), "paused"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("SubAgentStatus.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubAgentStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status SubAgentStatus
		want   bool
	}{
		{"pending is valid", SubAgentStatusPending, true},
		{"running is valid", SubAgentStatusRunning, true},
		{"completed is valid", SubAgentStatusCompleted, true},
		{"failed is valid", SubAgentStatusFailed, true},
		{"canceled is valid", SubAgentStatusCanceled, true},
		{"max_turns_exhausted is valid", SubAgentStatusMaxTurns, true},
		{"empty is invalid", SubAgentStatus(""), false},
		{"unknown is invalid", SubAgentStatus("paused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("SubAgentStatus.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSubAgentStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status SubAgentStatus
		want   bool
	}{
		{"pending is not terminal", SubAgentStatusPending, false},
		{"running is not terminal", SubAgentStatusRunning, false},
		{"completed is terminal", SubAgentStatusCompleted, true},
		{"failed is terminal", SubAgentStatusFailed, true},
		{"canceled is terminal", SubAgentStatusCanceled, true},
		{"max_turns_exhausted is terminal", SubAgentStatusMaxTurns, true},
		{"empty is not terminal", SubAgentStatus(""), false},
		{"unknown is not terminal", SubAgentStatus("paused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("SubAgentStatus.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSubAgentStatus_Validate(t *testing.T) {
	tests := []struct {
		name    string
		status  SubAgentStatus
		wantErr bool
	}{
		{"pending no error", SubAgentStatusPending, false},
		{"running no error", SubAgentStatusRunning, false},
		{"completed no error", SubAgentStatusCompleted, false},
		{"failed no error", SubAgentStatusFailed, false},
		{"canceled no error", SubAgentStatusCanceled, false},
		{"max_turns_exhausted no error", SubAgentStatusMaxTurns, false},
		{"empty returns error", SubAgentStatus(""), true},
		{"unknown returns error", SubAgentStatus("paused"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.status.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("SubAgentStatus.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSubAgentResult_IsSuccess(t *testing.T) {
	tests := []struct {
		name   string
		result SubAgentResult
		want   bool
	}{
		{
			name: "completed without error",
			result: SubAgentResult{
				AgentName: "reviewer",
				Output:    "all good",
				Status:    SubAgentStatusCompleted,
				Elapsed:   2 * time.Second,
			},
			want: true,
		},
		{
			name: "completed with error",
			result: SubAgentResult{
				AgentName: "reviewer",
				Err:       errors.New("timeout"),
				Status:    SubAgentStatusCompleted,
			},
			want: false,
		},
		{
			name: "failed without error field",
			result: SubAgentResult{
				AgentName: "reviewer",
				Status:    SubAgentStatusFailed,
			},
			want: false,
		},
		{
			name: "pending status",
			result: SubAgentResult{
				AgentName: "reviewer",
				Status:    SubAgentStatusPending,
			},
			want: false,
		},
		{
			name: "running status",
			result: SubAgentResult{
				AgentName: "reviewer",
				Status:    SubAgentStatusRunning,
			},
			want: false,
		},
		{
			name: "canceled status",
			result: SubAgentResult{
				AgentName: "reviewer",
				Status:    SubAgentStatusCanceled,
			},
			want: false,
		},
		{
			name: "max_turns_exhausted status",
			result: SubAgentResult{
				AgentName: "reviewer",
				Output:    "partial",
				Status:    SubAgentStatusMaxTurns,
			},
			want: false,
		},
		{
			name:   "zero value",
			result: SubAgentResult{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsSuccess(); got != tt.want {
				t.Errorf("SubAgentResult.IsSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
}
