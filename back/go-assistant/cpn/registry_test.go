package cpn

import (
	"testing"
)

// TestInvokable exercises REQ-GATE-001: a model is invokable iff
// lifecycle = 'active' AND license.status ∈ {approved-commercial,
// approved-non-commercial, restricted}. The table covers the full
// cartesian product of lifecycle × license_status (56 cases) per
// REQ-GATE-004 defense-in-depth.
func TestInvokable(t *testing.T) {
	lifecycles := []string{
		LifecycleDiscovered,
		LifecyclePendingLicenseReview,
		LifecycleRegistered,
		LifecycleActive,
		LifecycleDisabled,
		LifecycleDeprecated,
		LifecycleSunset,
		LifecycleRemoved,
	}
	licenses := []string{
		LicenseUnreviewed,
		LicenseReviewInProgress,
		LicenseApprovedCommercial,
		LicenseApprovedNonCommerc,
		LicenseRestricted,
		LicenseBlocked,
		LicenseUnknown,
	}
	approved := map[string]bool{
		LicenseApprovedCommercial: true,
		LicenseApprovedNonCommerc: true,
		LicenseRestricted:         true,
	}

	for _, lc := range lifecycles {
		for _, ls := range licenses {
			name := lc + "/" + ls
			want := lc == LifecycleActive && approved[ls]
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				m := &ModelRegistryEntry{
					Lifecycle: Lifecycle{State: lc},
					License:   License{Status: ls},
				}
				if got := m.Invokable(); got != want {
					t.Fatalf("Invokable()=%v want %v for lifecycle=%s license=%s", got, want, lc, ls)
				}
				if got := CanInvoke(m); got != want {
					t.Fatalf("CanInvoke()=%v want %v", got, want)
				}
			})
		}
	}

	t.Run("Nil_returns_false", func(t *testing.T) {
		t.Parallel()
		var m *ModelRegistryEntry
		if m.Invokable() {
			t.Fatal("nil receiver should return false")
		}
	})
}
