package planner

import "testing"

func TestPlanGate_ShouldDecompose(t *testing.T) {
	gate := NewPlanGate()

	tests := []struct {
		name string
		task string
		want bool
	}{
		// --- bypass cases (simple task) ---
		{"empty string", "", false},
		{"very short no keywords", "say hello", false},
		{"exactly 39 chars no keywords", "123456789012345678901234567890123456789", false},
		// --- decompose cases (complex keyword) ---
		{"has 'research'", "research AI papers", true},
		{"has 'find'", "find 3 relevant URLs", true},
		{"has 'analyze'", "analyze performance", true},
		{"has 'synthesize'", "synthesize results", true},
		{"has 'compare'", "compare two outputs", true},
		{"has 'summarize'", "summarize findings", true},
		{"has 'investigate'", "investigate the bug", true},
		{"has 'evaluate'", "evaluate options", true},
		// --- decompose cases (long task) ---
		{"40 chars no keywords — decompose", "12345678901234567890123456789012345678901", true},
		{"long task with keywords", "research the best Go concurrency patterns", true},
		// --- keyword case-insensitive ---
		{"keyword RESEARCH uppercase", "RESEARCH something", true},
		{"keyword Analyze mixed case", "Analyze the data", true},
		// --- uk spellings (still recognized via normalize) ---
		{"uk 'analyze' variant", "analyze the logs", true},
		{"uk 'summarize' variant", "summarize findings", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gate.ShouldDecompose(tt.task)
			if got != tt.want {
				t.Errorf("ShouldDecompose(%q) = %v, want %v", tt.task, got, tt.want)
			}
		})
	}
}
