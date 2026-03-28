package cpn

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors_AreDistinct(t *testing.T) {
	sentinels := []error{
		// v1.0
		ErrColorMismatch, ErrEmptyPlace, ErrInvalidArc, ErrDeadlock,
		ErrTimeout, ErrSpaceMismatch, ErrSpaceViolation, ErrSubNetFailed,
		ErrNoHITLWaiting, ErrInvalidNodeKind, ErrCentaurianGuard,
		// v1.1
		ErrValidationFailed, ErrCircuitOpen, ErrRateLimited,
		ErrProviderUnavailable, ErrBudgetExceeded, ErrHITLRejected,
		ErrHITLMisconfigured, ErrHITLMaxRevisions,
		// v1.2
		ErrBadRequest, ErrUnauthorized, ErrInsufficientCredits,
		ErrForbidden, ErrNotFound, ErrRequestTimeout, ErrPayloadTooLarge,
		ErrUnprocessableEntity, ErrEdgeTimeout, ErrProviderOverloaded,
		// v1.3
		ErrToolCallLoopExceeded, ErrDisallowedTool,
		// v1.4
		ErrSessionClosed, ErrHITLAlreadyRegistered,
	}
	if len(sentinels) != 33 {
		t.Fatalf("expected 33 sentinel errors, got %d — update this test when adding errors", len(sentinels))
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("error %d (%v) should not match error %d (%v)", i, a, j, b)
			}
		}
	}
}

func TestSentinelErrors_WrapCorrectly(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		// v1.0
		{"ErrColorMismatch", ErrColorMismatch},
		{"ErrEmptyPlace", ErrEmptyPlace},
		{"ErrInvalidArc", ErrInvalidArc},
		{"ErrDeadlock", ErrDeadlock},
		{"ErrTimeout", ErrTimeout},
		{"ErrSpaceMismatch", ErrSpaceMismatch},
		{"ErrSpaceViolation", ErrSpaceViolation},
		{"ErrSubNetFailed", ErrSubNetFailed},
		{"ErrNoHITLWaiting", ErrNoHITLWaiting},
		{"ErrInvalidNodeKind", ErrInvalidNodeKind},
		{"ErrCentaurianGuard", ErrCentaurianGuard},
		// v1.1
		{"ErrValidationFailed", ErrValidationFailed},
		{"ErrCircuitOpen", ErrCircuitOpen},
		{"ErrRateLimited", ErrRateLimited},
		{"ErrProviderUnavailable", ErrProviderUnavailable},
		{"ErrBudgetExceeded", ErrBudgetExceeded},
		{"ErrHITLRejected", ErrHITLRejected},
		{"ErrHITLMisconfigured", ErrHITLMisconfigured},
		{"ErrHITLMaxRevisions", ErrHITLMaxRevisions},
		// v1.2
		{"ErrBadRequest", ErrBadRequest},
		{"ErrUnauthorized", ErrUnauthorized},
		{"ErrInsufficientCredits", ErrInsufficientCredits},
		{"ErrForbidden", ErrForbidden},
		{"ErrNotFound", ErrNotFound},
		{"ErrRequestTimeout", ErrRequestTimeout},
		{"ErrPayloadTooLarge", ErrPayloadTooLarge},
		{"ErrUnprocessableEntity", ErrUnprocessableEntity},
		{"ErrEdgeTimeout", ErrEdgeTimeout},
		{"ErrProviderOverloaded", ErrProviderOverloaded},
		// v1.3
		{"ErrToolCallLoopExceeded", ErrToolCallLoopExceeded},
		{"ErrDisallowedTool", ErrDisallowedTool},
		// v1.4
		{"ErrSessionClosed", ErrSessionClosed},
		{"ErrHITLAlreadyRegistered", ErrHITLAlreadyRegistered},
	}
	for _, tt := range sentinels {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := fmt.Errorf("context: %w", tt.err)
			if !errors.Is(wrapped, tt.err) {
				t.Errorf("wrapped error should match sentinel: %v", tt.err)
			}
		})
	}
}
