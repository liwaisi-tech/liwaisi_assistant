package fanout

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestValidatePlan_Fixtures exercises the JSON fixtures checked into
// testdata/ so the round-trip schema-compatibility stays under test.
func TestValidatePlan_Fixtures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file    string
		wantErr error
	}{
		{"plan_happy.json", nil},
		{"plan_gate_violating.json", nil}, // structurally valid — gate denies at runtime.
		{"plan_cap_exceeded.json", ErrTooManyProbes},
		{"plan_command_too_long.json", ErrCommandTooLong},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("testdata", tc.file)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v", path, err)
			}
			var plan AwakeningProbePlan
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatalf("unmarshal %s: %v", path, err)
			}
			err = ValidatePlan(plan)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.file, err)
			}
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("%s: expected error %v, got nil", tc.file, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: expected errors.Is(%v), got %v", tc.file, tc.wantErr, err)
				}
			}
		})
	}
}
