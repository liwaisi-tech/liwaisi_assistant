package host

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// AllowAllHostGate is the v1 stub implementation of cpn.HostGate. It
// unconditionally approves every call. GAP-6 replaces this with a
// policy-backed gate without any adapter changes.
//
// Even with AllowAllHostGate the OSHostAdapter still enforces SEC-001
// (deny list) and SEC-002 (WriteFile jail).
type AllowAllHostGate struct{}

// Compile-time interface check.
var _ cpn.HostGate = (*AllowAllHostGate)(nil)

// Check always returns nil.
func (AllowAllHostGate) Check(_ context.Context, _ cpn.GateOp) error { return nil }

// NewAllowAllHostGate returns an AllowAllHostGate value.
func NewAllowAllHostGate() AllowAllHostGate { return AllowAllHostGate{} }
