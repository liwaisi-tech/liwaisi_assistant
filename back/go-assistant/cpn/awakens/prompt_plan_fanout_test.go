//go:build fanout_integration
// +build fanout_integration

// This test is gated behind the `fanout_integration` build tag because it
// imports cpn/awakens/fanout, a package still under construction by the
// Composer Engineer (spec-architecture-brae-awakening-probe-fanout.md §4.3).
// Flip the tag on in CI once fanout.ValidatePlan is shipped:
//
//	go test -tags=fanout_integration ./cpn/awakens/...
//
// The canonical regression that catches broken ExamplePlan without taking
// an import dependency on the in-flight package lives in prompt_test.go —
// see TestPromptPlan_ExampleParsesAsStructurallyValidPlan.

package awakens

import (
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/fanout"
)

// TestPromptPlan_ExampleIsValidPlan round-trips ExamplePlan through the
// canonical fanout.ValidatePlan. If a future prompt edit breaks the example
// (missing field, command too long, duplicate id, >16 probes, etc.) this
// test fires. REQ-009 + CON-002 + CON-003 regression guard.
func TestPromptPlan_ExampleIsValidPlan(t *testing.T) {
	t.Parallel()

	var p fanout.AwakeningProbePlan
	if err := json.Unmarshal([]byte(ExamplePlan), &p); err != nil {
		t.Fatalf("ExamplePlan JSON decode: %v", err)
	}
	if err := fanout.ValidatePlan(p); err != nil {
		t.Fatalf("fanout.ValidatePlan(ExamplePlan): %v", err)
	}
}
