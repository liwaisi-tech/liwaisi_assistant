package valueobject

// TeamEvaluation is the result of evaluating whether a task requires
// a multi-disciplinary team of subagents.
type TeamEvaluation struct {
	NeedsTeam  bool       `json:"needs_team"`
	Confidence float64    `json:"confidence"`
	Roles      []RoleSpec `json:"roles,omitempty"`
	Reasoning  string     `json:"reasoning"`
}

// RoleSpec describes a specialist role within a team.
type RoleSpec struct {
	Name        string `json:"name"`
	Perspective string `json:"perspective"`
	Instruction string `json:"instruction"`
}
