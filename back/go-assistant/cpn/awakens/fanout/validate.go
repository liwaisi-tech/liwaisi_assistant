package fanout

import (
	"fmt"
	"strings"
)

// ValidatePlan enforces CON-002 (MaxProbes), CON-003 (MaxCommandLen), and
// SEC-002 (no null byte, non-empty command/target, valid kind) on the probe
// plan. It is pure: no I/O, no mutation of the input.
//
// Errors are returned wrapping a sentinel (ErrEmptyPlan, ErrTooManyProbes,
// ErrCommandTooLong, ErrInvalidProbeKind, ErrInvalidProbe) so callers may
// discriminate via errors.Is.
func ValidatePlan(plan AwakeningProbePlan) error {
	if len(plan.Probes) == 0 {
		return ErrEmptyPlan
	}
	if len(plan.Probes) > MaxProbes {
		return fmt.Errorf("%w: got %d, max %d",
			ErrTooManyProbes, len(plan.Probes), MaxProbes)
	}
	for i, p := range plan.Probes {
		if err := validateProbe(i, p); err != nil {
			return err
		}
	}
	return nil
}

// validateProbe runs the per-entry structural checks. Kept package-private
// so tests drive it via ValidatePlan and the exported contract stays minimal.
func validateProbe(idx int, p AwakeningProbeEntry) error {
	// SEC-002: Command must not be empty.
	if strings.TrimSpace(p.Command) == "" {
		return fmt.Errorf("%w: probe[%d] id=%q: empty command",
			ErrInvalidProbe, idx, p.ID)
	}
	// CON-003: size cap is measured in raw bytes, before trimming, so a
	// padded command is rejected too.
	if len(p.Command) > MaxCommandLen {
		return fmt.Errorf("%w: probe[%d] id=%q: len=%d max=%d",
			ErrCommandTooLong, idx, p.ID, len(p.Command), MaxCommandLen)
	}
	// SEC-002: null-byte rejection. Newlines are permitted because some
	// legitimate shell idioms (heredoc, multi-line pipelines) include them
	// and the HostGate carries the real policy burden — but a NUL byte is
	// never a valid introspection command and is a classic smuggling
	// vector.
	if strings.ContainsRune(p.Command, '\x00') {
		return fmt.Errorf("%w: probe[%d] id=%q: null byte in command",
			ErrInvalidProbe, idx, p.ID)
	}
	// SEC-002: Target must be non-empty — it is the key under which the
	// reducer files the result into binaries[] / capabilities[].
	if strings.TrimSpace(p.Target) == "" {
		return fmt.Errorf("%w: probe[%d] id=%q: empty target",
			ErrInvalidProbe, idx, p.ID)
	}
	// SEC-002: Kind must be one of the two recognised classifiers so the
	// reducer routes the result deterministically.
	switch p.Kind {
	case ProbeKindBinary, ProbeKindCapability:
	default:
		return fmt.Errorf("%w: probe[%d] id=%q: kind=%q",
			ErrInvalidProbeKind, idx, p.ID, p.Kind)
	}
	return nil
}
