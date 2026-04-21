package persist

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Classify maps (path, mode) to an ArtefactClassification per spec §3
// REQ-005. The mapping is deterministic and intentionally coarse — callers
// that need finer-grained typing should set WriteIntent.Classification
// explicitly.
//
// Rules (first match wins):
//  1. Executable bits set (mode&0o111) and no known source extension → binary.
//  2. File extension in a curated source set → source.
//  3. File extension in a curated config set → config.
//  4. Extension .log or residing under a "log/" path segment → log.
//  5. Extension .1-.9, .man or residing under "man/" → man.
//  6. Fallback → other.
func Classify(path string, mode fs.FileMode) ArtefactClassification {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	dir := strings.ToLower(filepath.ToSlash(filepath.Dir(path)))

	if mode&0o111 != 0 && !isSourceExt(ext) {
		return ClassBinary
	}

	if isSourceExt(ext) {
		return ClassSource
	}

	if isConfigExt(ext) || isConfigName(name) {
		return ClassConfig
	}

	if ext == ".log" || strings.Contains(dir, "/log/") || strings.HasSuffix(dir, "/log") || strings.Contains(dir, "/logs/") || strings.HasSuffix(dir, "/logs") {
		return ClassLog
	}

	if ext == ".man" || strings.Contains(dir, "/man/") || strings.HasSuffix(dir, "/man") || isManPageExt(ext) {
		return ClassMan
	}

	return ClassOther
}

// isSourceExt lists extensions treated as human-authored source code.
func isSourceExt(ext string) bool {
	switch ext {
	case ".go", ".py", ".rs", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".java", ".kt", ".scala", ".rb", ".php", ".c", ".h", ".cc", ".cpp",
		".hpp", ".hxx", ".cs", ".swift", ".m", ".mm", ".sh", ".bash", ".zsh",
		".fish", ".lua", ".pl", ".erl", ".ex", ".exs", ".hs", ".ml", ".clj",
		".sql", ".r", ".dart", ".nim", ".zig", ".vue", ".svelte", ".proto",
		".md", ".rst", ".tex":
		return true
	}
	return false
}

// isConfigExt lists extensions treated as configuration files.
func isConfigExt(ext string) bool {
	switch ext {
	case ".yaml", ".yml", ".toml", ".json", ".ini", ".conf", ".cfg",
		".env", ".properties", ".hcl", ".tf", ".tfvars":
		return true
	}
	return false
}

// isConfigName catches extension-less config files by common base name.
func isConfigName(name string) bool {
	switch name {
	case ".env", "config", "dockerfile", "makefile", ".gitignore",
		".dockerignore", ".editorconfig":
		return true
	}
	return false
}

// isManPageExt matches .1 through .9 (classic man section extensions).
func isManPageExt(ext string) bool {
	if len(ext) != 2 || ext[0] != '.' {
		return false
	}
	c := ext[1]
	return c >= '1' && c <= '9'
}
