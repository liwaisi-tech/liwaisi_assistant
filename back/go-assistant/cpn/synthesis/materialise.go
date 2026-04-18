package synthesis

import (
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// Materialise converts a persisted CPNTopology into a runnable *cpn.CPN
// using ONLY safe primitives. Resolves every guard/executor/factory name
// through a FuncRegistry built from the SafeRegistry.
//
// Callers (the NodeKindInstantiate handler) invoke this AFTER a successful
// Lint pass, so Materialise can assume the topology is already safe-
// compliant. It still surfaces lookup errors so a stale safe registry
// (primitive removed post-persist) returns ErrDeprecatedDependency.
func Materialise(topo *persist.CPNTopology, safe *SafeRegistry) (*cpn.CPN, error) {
	if topo == nil {
		return nil, fmt.Errorf("synthesis: nil topology")
	}
	if safe == nil {
		return nil, fmt.Errorf("synthesis: nil SafeRegistry")
	}
	reg := safe.BuildSafeFuncRegistry()
	c, err := persist.UnmarshalCPN(topo, reg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", cpn.ErrDeprecatedDependency, err)
	}
	return c, nil
}
