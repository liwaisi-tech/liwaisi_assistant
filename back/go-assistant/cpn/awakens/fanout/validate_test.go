package fanout

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePlan(t *testing.T) {
	t.Parallel()

	okProbe := AwakeningProbeEntry{
		ID:      "cmd-sh",
		Kind:    ProbeKindBinary,
		Target:  "sh",
		Command: "command -v sh",
	}

	tests := []struct {
		name    string
		plan    AwakeningProbePlan
		wantErr error
	}{
		{
			name:    "empty plan rejected",
			plan:    AwakeningProbePlan{Probes: nil},
			wantErr: ErrEmptyPlan,
		},
		{
			name: "too many probes rejected",
			plan: AwakeningProbePlan{
				Probes: func() []AwakeningProbeEntry {
					ps := make([]AwakeningProbeEntry, MaxProbes+1)
					for i := range ps {
						ps[i] = okProbe
					}
					return ps
				}(),
			},
			wantErr: ErrTooManyProbes,
		},
		{
			name: "command exceeds MaxCommandLen",
			plan: AwakeningProbePlan{
				Probes: []AwakeningProbeEntry{{
					ID:      "long",
					Kind:    ProbeKindBinary,
					Target:  "x",
					Command: strings.Repeat("a", MaxCommandLen+1),
				}},
			},
			wantErr: ErrCommandTooLong,
		},
		{
			name: "null byte in command rejected",
			plan: AwakeningProbePlan{
				Probes: []AwakeningProbeEntry{{
					ID:      "nul",
					Kind:    ProbeKindBinary,
					Target:  "x",
					Command: "echo hi\x00boom",
				}},
			},
			wantErr: ErrInvalidProbe,
		},
		{
			name: "empty command rejected",
			plan: AwakeningProbePlan{
				Probes: []AwakeningProbeEntry{{
					ID:      "empty",
					Kind:    ProbeKindBinary,
					Target:  "x",
					Command: "   ",
				}},
			},
			wantErr: ErrInvalidProbe,
		},
		{
			name: "empty target rejected",
			plan: AwakeningProbePlan{
				Probes: []AwakeningProbeEntry{{
					ID:      "e",
					Kind:    ProbeKindBinary,
					Target:  "",
					Command: "command -v x",
				}},
			},
			wantErr: ErrInvalidProbe,
		},
		{
			name: "invalid kind rejected",
			plan: AwakeningProbePlan{
				Probes: []AwakeningProbeEntry{{
					ID:      "k",
					Kind:    "exec",
					Target:  "x",
					Command: "command -v x",
				}},
			},
			wantErr: ErrInvalidProbeKind,
		},
		{
			name: "happy path single binary",
			plan: AwakeningProbePlan{
				Probes: []AwakeningProbeEntry{okProbe},
			},
			wantErr: nil,
		},
		{
			name: "happy path mixed kinds",
			plan: AwakeningProbePlan{
				Probes: []AwakeningProbeEntry{
					okProbe,
					{ID: "cap-git", Kind: ProbeKindCapability, Target: "git",
						Command: "command -v git"},
				},
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePlan(tc.plan)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected errors.Is(err, %v), got %v", tc.wantErr, err)
			}
		})
	}
}
