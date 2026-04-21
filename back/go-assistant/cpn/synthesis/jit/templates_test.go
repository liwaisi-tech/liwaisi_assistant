package jit

import (
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestSelectTemplate(t *testing.T) {
	tests := []struct {
		name     string
		override TemplateID
		intent   cpn.Intent
		want     TemplateID
	}{
		{"override-parallel", TemplateParallelFanout, cpn.Intent{NL: "fetch then parse"}, TemplateParallelFanout},
		{"override-sequential", TemplateSequentialPipeline, cpn.Intent{NL: "x"}, TemplateSequentialPipeline},
		{"en-pipeline-then", TemplateAuto, cpn.Intent{NL: "fetch then parse", Lang: "en"}, TemplateSequentialPipeline},
		{"en-pipeline-arrow", TemplateAuto, cpn.Intent{NL: "fetch → parse", Lang: ""}, TemplateSequentialPipeline},
		{"en-no-cue", TemplateAuto, cpn.Intent{NL: "summarise this", Lang: "en"}, TemplateParallelFanout},
		{"es-with-luego", TemplateAuto, cpn.Intent{NL: "descarga y luego extrae", Lang: "es-CO"}, TemplateParallelFanout},
		{"en-us-variant", TemplateAuto, cpn.Intent{NL: "fetch then run", Lang: "en-US"}, TemplateSequentialPipeline},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectTemplate(tc.override, tc.intent); got != tc.want {
				t.Errorf("selectTemplate = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildParallelFanout_Counts(t *testing.T) {
	matches := []cpn.ToolMatch{{QualifiedName: "a"}, {QualifiedName: "b"}}
	d := buildParallelFanout("jit-x", matches)
	if len(d.Places) != 4 {
		t.Errorf("places = %d, want 4", len(d.Places))
	}
	if len(d.Transitions) != 4 {
		t.Errorf("transitions = %d, want 4", len(d.Transitions))
	}
}

func TestBuildSequentialPipeline_Counts(t *testing.T) {
	matches := []cpn.ToolMatch{{QualifiedName: "a"}, {QualifiedName: "b"}, {QualifiedName: "c"}}
	d := buildSequentialPipeline("jit-x", matches)
	// p-input + p-mid-0 + p-mid-1 + p-output = 4 places
	if len(d.Places) != 4 {
		t.Errorf("places = %d, want 4", len(d.Places))
	}
	// 3 steps
	if len(d.Transitions) != 3 {
		t.Errorf("transitions = %d, want 3", len(d.Transitions))
	}
}
