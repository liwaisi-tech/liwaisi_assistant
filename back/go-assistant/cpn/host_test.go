package cpn

import (
	"errors"
	"testing"
	"time"
)

// TestHostError_IsAndUnwrap verifies stable Code-based equality and chain
// traversal.
func TestHostError_IsAndUnwrap(t *testing.T) {
	cause := errors.New("boom")
	he := NewHostError(HostErrCodeTimeout, "took too long", cause)

	if !errors.Is(he, ErrTimeoutHost) {
		t.Fatalf("expected errors.Is(he, ErrTimeoutHost) == true")
	}
	if errors.Is(he, ErrGateDenied) {
		t.Fatalf("expected errors.Is(he, ErrGateDenied) == false")
	}
	if !errors.Is(he, cause) {
		t.Fatalf("expected Unwrap chain to include cause")
	}
	if HostErrorCode(he) != HostErrCodeTimeout {
		t.Fatalf("HostErrorCode mismatch: got %q", HostErrorCode(he))
	}
	if HostErrorCode(nil) != "" {
		t.Fatalf("HostErrorCode(nil) should be empty string")
	}
}

// TestHostError_String covers the Error() format.
func TestHostError_String(t *testing.T) {
	he := NewHostError("x", "bad thing", nil)
	if got := he.Error(); got != "x: bad thing" {
		t.Fatalf("Error() = %q", got)
	}
	he = NewHostError("x", "bad thing", errors.New("cause"))
	if got := he.Error(); got != "x: bad thing: cause" {
		t.Fatalf("Error() with cause = %q", got)
	}
	empty := &HostError{Code: "only_code"}
	if empty.Error() != "only_code" {
		t.Fatalf("Error() with empty message = %q", empty.Error())
	}
}

// TestIsShellColor verifies the color-set helper used by Validate.
func TestIsShellColor(t *testing.T) {
	shell := []ColorSet{ColorShellCmd, ColorShellChunk, ColorShellResult, ColorHostFact, ColorProcess}
	for _, c := range shell {
		if !IsShellColor(c) {
			t.Errorf("IsShellColor(%s) = false, want true", c)
		}
	}
	for _, c := range []ColorSet{ColorString, ColorJSON, ColorArtifact, ColorError} {
		if IsShellColor(c) {
			t.Errorf("IsShellColor(%s) = true, want false", c)
		}
	}
}

// TestBashValidate_TableDriven exercises all CON-001/CON-002 branches of
// Validate for NodeKindBash transitions.
func TestBashValidate_TableDriven(t *testing.T) {
	mkPlace := func(id string, col ColorSet) *Place {
		return NewPlace(id, col, SpaceComputation)
	}

	tests := []struct {
		name    string
		places  map[string]*Place
		trans   map[string]*Transition
		wantErr bool
		wantMsg string
	}{
		{
			name: "valid_bash_transition",
			places: map[string]*Place{
				"in":  mkPlace("in", ColorShellCmd),
				"out": mkPlace("out", ColorShellResult),
			},
			trans: map[string]*Transition{
				"t-bash": {
					ID:           "t-bash",
					Kind:         NodeKindBash,
					InputPlaces:  []string{"in"},
					OutputPlaces: []string{"out"},
					BashConfig: &BashConfig{
						Command: "echo",
						Args:    []string{"hi"},
						Timeout: time.Second,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "nil_config",
			places: map[string]*Place{
				"in":  mkPlace("in", ColorShellCmd),
				"out": mkPlace("out", ColorShellResult),
			},
			trans: map[string]*Transition{
				"t-bash": {
					ID:           "t-bash",
					Kind:         NodeKindBash,
					InputPlaces:  []string{"in"},
					OutputPlaces: []string{"out"},
				},
			},
			wantErr: true,
			wantMsg: "bash.missing_config",
		},
		{
			name: "empty_command",
			places: map[string]*Place{
				"in":  mkPlace("in", ColorShellCmd),
				"out": mkPlace("out", ColorShellResult),
			},
			trans: map[string]*Transition{
				"t-bash": {
					ID:           "t-bash",
					Kind:         NodeKindBash,
					InputPlaces:  []string{"in"},
					OutputPlaces: []string{"out"},
					BashConfig:   &BashConfig{Command: ""},
				},
			},
			wantErr: true,
			wantMsg: "bash.missing_command",
		},
		{
			name: "bad_output_color",
			places: map[string]*Place{
				"in":  mkPlace("in", ColorShellCmd),
				"out": mkPlace("out", ColorString),
			},
			trans: map[string]*Transition{
				"t-bash": {
					ID:           "t-bash",
					Kind:         NodeKindBash,
					InputPlaces:  []string{"in"},
					OutputPlaces: []string{"out"},
					BashConfig:   &BashConfig{Command: "echo"},
				},
			},
			wantErr: true,
			wantMsg: "bash.invalid_output_color",
		},
		{
			name: "missing_error_place",
			places: map[string]*Place{
				"in":  mkPlace("in", ColorShellCmd),
				"out": mkPlace("out", ColorShellResult),
			},
			trans: map[string]*Transition{
				"t-bash": {
					ID:           "t-bash",
					Kind:         NodeKindBash,
					InputPlaces:  []string{"in"},
					OutputPlaces: []string{"out"},
					ErrorPlace:   "p-missing",
					BashConfig:   &BashConfig{Command: "echo"},
				},
			},
			wantErr: true,
			wantMsg: "non-existent error place",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.places, tc.trans)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr {
				if !containsErrMsg(err, tc.wantMsg) {
					t.Fatalf("expected error to contain %q, got %v", tc.wantMsg, err)
				}
			}
		})
	}
}

func containsErrMsg(err error, substr string) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), substr)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
