package tools

import (
	_ "embed"
	"sync"
	"unicode"

	"gopkg.in/yaml.v3"
)

// toolboxCatalogEntry is the YAML-backed aggregate descriptor for one
// toolbox namespace (spec §4.3). Title/Summary hydrate ToolboxManifest; when
// a namespace is absent from the catalog the registry falls back to
// Title = Capitalised(Namespace) and Summary = "".
type toolboxCatalogEntry struct {
	Namespace string `yaml:"namespace"`
	Title     string `yaml:"title"`
	Summary   string `yaml:"summary"`
}

type toolboxCatalogFile struct {
	Version int                   `yaml:"version"`
	Entries []toolboxCatalogEntry `yaml:"entries"`
}

//go:embed toolboxes.yaml
var embeddedToolboxesYAML []byte

var (
	toolboxCatalogOnce sync.Once
	toolboxCatalog     map[string]toolboxCatalogEntry
)

func loadToolboxCatalog() map[string]toolboxCatalogEntry {
	toolboxCatalogOnce.Do(func() {
		toolboxCatalog = parseToolboxCatalog(embeddedToolboxesYAML)
	})
	return toolboxCatalog
}

func parseToolboxCatalog(buf []byte) map[string]toolboxCatalogEntry {
	out := make(map[string]toolboxCatalogEntry)
	if len(buf) == 0 {
		return out
	}
	var file toolboxCatalogFile
	if err := yaml.Unmarshal(buf, &file); err != nil {
		// Catalog is a developer-owned fixture — if it fails to parse, fall
		// back to namespace-derived defaults rather than block startup.
		return out
	}
	for _, e := range file.Entries {
		if e.Namespace == "" {
			continue
		}
		out[e.Namespace] = e
	}
	return out
}

// toolboxTitleFor returns the display title for namespace, preferring the
// catalog entry; otherwise capitalising the first rune of namespace.
func toolboxTitleFor(namespace string) string {
	if e, ok := loadToolboxCatalog()[namespace]; ok && e.Title != "" {
		return e.Title
	}
	return capitaliseFirst(namespace)
}

// toolboxSummaryFor returns the one-line summary for namespace from the
// catalog, or "" when unknown.
func toolboxSummaryFor(namespace string) string {
	if e, ok := loadToolboxCatalog()[namespace]; ok {
		return e.Summary
	}
	return ""
}

func capitaliseFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
