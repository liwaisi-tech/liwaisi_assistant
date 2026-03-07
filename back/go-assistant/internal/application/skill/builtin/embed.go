// Package builtin provides embedded built-in skills that ship with the binary.
package builtin

import "embed"

// SkillsFS contains the built-in skill directories (e.g. skill-creator/, subagent-creator/).
// Each subdirectory must contain a valid SKILL.md file.
//
//go:embed skill-creator subagent-creator
var SkillsFS embed.FS
