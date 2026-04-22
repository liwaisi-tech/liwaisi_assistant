package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// Compile-time assertion: *Registry satisfies cpn.ToolboxCatalogPort so the
// session service can pass a Registry straight into the CPN field.
var _ cpn.ToolboxCatalogPort = (*Registry)(nil)

// RenderCatalogue implements cpn.ToolboxCatalogPort. The returned block is
// plain text with a stable shape the synthesize system prompt appends
// verbatim:
//
//	## Toolboxes available
//	### <namespace> (<tool_count> tools): <summary>
//	  - <qualified_name>: <description>
//	      params: <single-line JSON schema>
//	  - ...
//
// Tools whose name or qualified-name match the filter pass through; when
// filter.ToolNames is empty every non-deprecated entry is rendered.
// Toolboxes with no surviving tools are omitted.
func (r *Registry) RenderCatalogue(ctx context.Context, filter cpn.ToolboxCatalogFilter) string {
	boxes := r.Toolboxes(ctx)
	if len(boxes) == 0 {
		return ""
	}

	wanted := make(map[string]struct{}, len(filter.ToolNames))
	for _, n := range filter.ToolNames {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		wanted[strings.ToLower(n)] = struct{}{}
	}
	accept := func(e *ToolEntry) bool {
		if len(wanted) == 0 {
			return true
		}
		if _, ok := wanted[strings.ToLower(e.QualifiedName())]; ok {
			return true
		}
		if _, ok := wanted[strings.ToLower(e.Name)]; ok {
			return true
		}
		return false
	}

	// Sort toolboxes by namespace so the rendered block is deterministic
	// regardless of Toolboxes()'s internal ordering.
	sort.Slice(boxes, func(i, j int) bool {
		return boxes[i].Namespace < boxes[j].Namespace
	})

	var b strings.Builder
	b.Grow(256 + len(boxes)*192)
	b.WriteString("## Toolboxes available\n\n")

	any := false
	for _, box := range boxes {
		entries := r.ListByToolbox(ctx, box.Namespace)
		if len(entries) == 0 {
			continue
		}
		var kept []*ToolEntry
		for _, e := range entries {
			if e == nil {
				continue
			}
			if !accept(e) {
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			continue
		}
		any = true

		summary := strings.TrimSpace(box.Summary)
		if summary == "" {
			summary = strings.TrimSpace(box.Title)
		}
		if summary != "" && !strings.HasSuffix(summary, ".") {
			summary += "."
		}
		fmt.Fprintf(&b, "### %s (%d tools)", box.Namespace, box.ToolCount)
		if summary != "" {
			fmt.Fprintf(&b, ": %s", summary)
		}
		b.WriteByte('\n')

		for _, e := range kept {
			desc := ""
			if e.Schema != nil {
				desc = strings.TrimSpace(e.Schema.Description)
			}
			if desc == "" {
				desc = strings.TrimSpace(e.HelpText)
			}
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(&b, "  - %s: %s\n", e.QualifiedName(), oneLine(desc))
			if e.Schema != nil && len(e.Schema.Parameters) > 0 {
				fmt.Fprintf(&b, "      params: %s\n", compactJSON(e.Schema.Parameters))
			}
		}
		b.WriteByte('\n')
	}

	if !any {
		return ""
	}
	return b.String()
}

// oneLine collapses whitespace runs so a multi-line description renders
// on a single bullet line. Keeps the synthesize prompt compact.
func oneLine(s string) string {
	fs := strings.Fields(s)
	return strings.Join(fs, " ")
}

// compactJSON strips embedded newlines/indentation from raw JSON so it fits
// on one line. Returns the input verbatim when it is already compact (no
// re-parsing to avoid round-trip mutation of the catalogue).
func compactJSON(raw []byte) string {
	s := string(raw)
	if !strings.ContainsAny(s, "\n\t") {
		return s
	}
	return oneLine(s)
}
