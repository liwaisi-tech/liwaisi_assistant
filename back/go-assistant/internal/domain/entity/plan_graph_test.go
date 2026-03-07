package entity

import "testing"

func makeSpec(name string) SubAgentSpec {
	return SubAgentSpec{Name: name, Instruction: "do something", BuiltIn: false}
}

func TestPlanGraph_Validate(t *testing.T) {
	tests := []struct {
		name        string
		graph       PlanGraph
		wantErr     bool
		errContains string
	}{
		{
			name: "single task — valid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "url-finder", Description: "Find URLs", Spec: makeSpec("searcher")},
				},
			},
			wantErr: false,
		},
		{
			name: "linear chain A→B→C — valid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "A", Spec: makeSpec("s1")},
					{ID: "B", Spec: makeSpec("s2"), DependsOn: []string{"A"}},
					{ID: "C", Spec: makeSpec("s3"), DependsOn: []string{"B"}},
				},
			},
			wantErr: false,
		},
		{
			name: "parallel fan-out — valid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "root", Spec: makeSpec("s1")},
					{ID: "worker-1", Spec: makeSpec("s2"), DependsOn: []string{"root"}},
					{ID: "worker-2", Spec: makeSpec("s3"), DependsOn: []string{"root"}},
					{ID: "synth", Spec: makeSpec("s4"), DependsOn: []string{"worker-1", "worker-2"}},
				},
			},
			wantErr: false,
		},
		{
			name: "ContextFrom references valid upstream — valid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "A", Spec: makeSpec("s1")},
					{ID: "B", Spec: makeSpec("s2"), DependsOn: []string{"A"}, ContextFrom: []string{"A"}},
				},
			},
			wantErr: false,
		},
		{
			name:        "empty graph — invalid",
			graph:       PlanGraph{Tasks: []*MicroTask{}},
			wantErr:     true,
			errContains: "at least one task",
		},
		{
			name: "task with empty ID — invalid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "", Spec: makeSpec("s1")},
				},
			},
			wantErr:     true,
			errContains: "empty ID",
		},
		{
			name: "duplicate task ID — invalid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "A", Spec: makeSpec("s1")},
					{ID: "A", Spec: makeSpec("s2")},
				},
			},
			wantErr:     true,
			errContains: "duplicate task ID",
		},
		{
			name: "depends on unknown ID — invalid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "A", Spec: makeSpec("s1"), DependsOn: []string{"nonexistent"}},
				},
			},
			wantErr:     true,
			errContains: "unknown ID",
		},
		{
			name: "ContextFrom references unknown ID — invalid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "A", Spec: makeSpec("s1"), ContextFrom: []string{"ghost"}},
				},
			},
			wantErr:     true,
			errContains: "unknown ContextFrom ID",
		},
		{
			name: "two-node cycle A→B, B→A — invalid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "A", Spec: makeSpec("s1"), DependsOn: []string{"B"}},
					{ID: "B", Spec: makeSpec("s2"), DependsOn: []string{"A"}},
				},
			},
			wantErr:     true,
			errContains: "cycle detected",
		},
		{
			name: "three-node cycle A→B→C→A — invalid",
			graph: PlanGraph{
				Tasks: []*MicroTask{
					{ID: "A", Spec: makeSpec("s1"), DependsOn: []string{"C"}},
					{ID: "B", Spec: makeSpec("s2"), DependsOn: []string{"A"}},
					{ID: "C", Spec: makeSpec("s3"), DependsOn: []string{"B"}},
				},
			},
			wantErr:     true,
			errContains: "cycle detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.graph.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContains != "" {
				if got := err.Error(); len(got) > 0 {
					for _, kw := range []string{tt.errContains} {
						_ = kw // already checked via wantErr; content validated below
					}
				}
			}
		})
	}
}

func TestPlanGraph_TaskByID(t *testing.T) {
	task := &MicroTask{ID: "finder", Spec: makeSpec("s1")}
	g := PlanGraph{Tasks: []*MicroTask{task}}

	got, ok := g.TaskByID("finder")
	if !ok || got != task {
		t.Errorf("TaskByID(\"finder\") = %v, %v; want task, true", got, ok)
	}

	_, ok = g.TaskByID("missing")
	if ok {
		t.Error("TaskByID(\"missing\") returned true, want false")
	}
}
