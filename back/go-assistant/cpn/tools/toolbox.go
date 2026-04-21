package tools

import "time"

// ToolboxManifest is the aggregate descriptor of one toolbox
// (spec-architecture-brae-toolbox-taxonomy.md §4.3, REQ-005). It is derived
// state, computed on demand from registry contents; consumers cache as needed.
type ToolboxManifest struct {
	Namespace          string    `json:"namespace"`
	Title              string    `json:"title"`
	Summary            string    `json:"summary"`
	Hashtags           []string  `json:"hashtags"`
	ToolCount          int       `json:"tool_count"`
	LatestRegisteredAt time.Time `json:"latest_registered_at"`
}
