package postgres

import (
	"strings"
	"testing"
	"time"
)

// fakeRow fulfils the Scan interface used by scanEntry without needing a
// live pgx pool. Each call to Scan consumes the next pre-baked value set.
type fakeRow struct {
	values []any
}

func (f *fakeRow) Scan(dest ...any) error {
	for i, d := range dest {
		if i >= len(f.values) {
			break
		}
		assignScan(d, f.values[i])
	}
	return nil
}

// assignScan is a toy assignment helper: it writes the source value into
// the destination pointer. It handles the narrow set of types scanEntry
// targets (strings, *strings, *time.Time, bool, int, float64, []byte).
func assignScan(dest, src any) {
	switch d := dest.(type) {
	case *string:
		if s, ok := src.(string); ok {
			*d = s
		}
	case **string:
		if src == nil {
			*d = nil
			return
		}
		if s, ok := src.(string); ok {
			tmp := s
			*d = &tmp
		}
	case *time.Time:
		if t, ok := src.(time.Time); ok {
			*d = t
		}
	case **time.Time:
		if src == nil {
			*d = nil
			return
		}
		if t, ok := src.(time.Time); ok {
			tmp := t
			*d = &tmp
		}
	case *bool:
		if b, ok := src.(bool); ok {
			*d = b
		}
	case *int:
		if i, ok := src.(int); ok {
			*d = i
		}
	case *float64:
		if f, ok := src.(float64); ok {
			*d = f
		}
	case *[]byte:
		if b, ok := src.([]byte); ok {
			*d = b
		}
	}
}

// baseScanValues returns a values-set scanEntry is happy with. Callers
// override individual JSONB columns to test corruption handling.
func baseScanValues() []any {
	now := time.Now().UTC()
	return []any{
		"uuid-1",       // id
		"vendor/model", // registry_id
		"vendor",       // vendor
		"family",       // family
		"1.0",          // version
		(*string)(nil), // variant
		"Display",      // display_name
		"",             // description
		(*string)(nil), // hugging_face_id
		[]byte(`{"input":["text"],"output":["text"]}`), // modalities
		[]byte(`{}`),          // capabilities
		0,                     // context_length
		"",                    // tokenizer
		[]byte(`[]`),          // supported_parameters
		[]byte(`{}`),          // default_parameters
		0.0,                   // pricing_input_per_token
		0.0,                   // pricing_output_per_token
		"USD",                 // pricing_currency
		"proprietary-api",     // license_kind
		(*string)(nil),        // license_spdx_id
		(*string)(nil),        // license_community_slug
		(*string)(nil),        // license_name
		(*string)(nil),        // license_url
		"manual",              // license_source
		"approved-commercial", // license_status
		(*string)(nil),        // license_reviewed_by
		(*time.Time)(nil),     // license_reviewed_at
		"active",              // lifecycle_state
		now,                   // lifecycle_registered_at
		(*time.Time)(nil),     // lifecycle_activated_at
		(*time.Time)(nil),     // lifecycle_deprecated_at
		(*time.Time)(nil),     // lifecycle_sunset_at
		(*string)(nil),        // lifecycle_replaced_by
		(*string)(nil),        // lifecycle_reason
		[]byte(`[]`),          // routes
		[]byte(`{}`),          // source_metadata
		now,                   // created_at
		now,                   // updated_at
		false,                 // is_product_default
	}
}

// TestScanEntry_CorruptRoutes_PropagatesError — AC-004.
// routes is load-bearing for invocation; malformed JSON must not default
// silently to an empty slice.
func TestScanEntry_CorruptRoutes_PropagatesError(t *testing.T) {
	vals := baseScanValues()
	// routes is index 34 in the column list (0-based; count matches
	// modelSelectColumns in model_registry.go).
	vals[34] = []byte(`{ this is not json`)
	row := &fakeRow{values: vals}

	_, err := scanEntry(row)
	if err == nil {
		t.Fatalf("expected scanEntry to return an error on corrupt routes, got nil")
	}
	if !strings.Contains(err.Error(), "routes") {
		t.Fatalf("expected error to mention routes column, got %v", err)
	}
}

// TestScanEntry_CorruptAdvisoryColumns_Defaults — the advisory columns
// (modalities, capabilities, supported_parameters, default_parameters,
// source_metadata) retain defaulting behaviour so a corrupt advisory
// field does not brick the admin UI.
func TestScanEntry_CorruptAdvisoryColumns_Defaults(t *testing.T) {
	vals := baseScanValues()
	vals[9] = []byte(`not json`)  // modalities
	vals[10] = []byte(`not json`) // capabilities
	row := &fakeRow{values: vals}

	e, err := scanEntry(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(e.Modalities.Input) == 0 {
		t.Fatalf("expected modalities to default to {text,text}")
	}
}

// TestScanEntry_HappyPath sanity-checks the fakeRow harness itself by
// running a well-formed row and confirming the decoded entry.
func TestScanEntry_HappyPath(t *testing.T) {
	row := &fakeRow{values: baseScanValues()}
	e, err := scanEntry(row)
	if err != nil {
		t.Fatalf("scanEntry: %v", err)
	}
	if e.RegistryID != "vendor/model" {
		t.Fatalf("registry id: %q", e.RegistryID)
	}
	if !e.Invokable() {
		t.Fatalf("expected active+approved to be Invokable")
	}
}
