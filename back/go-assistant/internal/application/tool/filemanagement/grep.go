package filemanagement

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	grepDefaultMaxResults = 100
	grepMaxMaxResults     = 500
	grepMaxContextLines   = 5
	grepConcurrency       = 10
	grepBinaryDetectSize  = 512
)

type grepArgs struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path"`
	CaseInsensitive bool   `json:"case_insensitive"`
	Include         string `json:"include"`
	ContextLines    int    `json:"context_lines"`
	MaxResults      int    `json:"max_results"`
}

type grepMatch struct {
	File          string   `json:"file"`
	Line          int      `json:"line"`
	Content       string   `json:"content"`
	ContextBefore []string `json:"context_before,omitempty"`
	ContextAfter  []string `json:"context_after,omitempty"`
}

type grepSummary struct {
	TotalMatches  int  `json:"total_matches"`
	FilesSearched int  `json:"files_searched"`
	FilesMatched  int  `json:"files_matched"`
	FilesSkipped  int  `json:"files_skipped"`
	Truncated     bool `json:"truncated"`
}

type grepResult struct {
	Pattern string      `json:"pattern"`
	Path    string      `json:"path"`
	Results []grepMatch `json:"results"`
	Summary grepSummary `json:"summary"`
}

func registerGrep(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "grep",
			Description: "Search for content matching a regular expression pattern inside files within the workspace. " +
				"Searches files concurrently for fast results. When searching a directory, recursively traverses " +
				"all files. Supports context lines (lines before/after each match) for understanding surrounding " +
				"code. Uses Go's regexp syntax. Binary files are skipped automatically. Maximum 500 matches returned.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {
						"type": "string",
						"description": "Regular expression pattern to search for. Uses RE2 regular expression syntax. Example: 'func\\s+main', 'TODO:', 'import\\s+\"fmt\"'"
					},
					"path": {
						"type": "string",
						"description": "Relative path to a file or directory to search in. If a directory, searches all files recursively. Defaults to workspace root if empty. Example: 'internal/application/'"
					},
					"case_insensitive": {
						"type": "boolean",
						"description": "If true, perform case-insensitive matching. Default: false.",
						"default": false
					},
					"include": {
						"type": "string",
						"description": "Glob pattern to filter which files to search. Only files matching this pattern are searched. Example: '*.go' to search only Go files."
					},
					"context_lines": {
						"type": "integer",
						"description": "Number of lines to show before and after each match (0-5). Default: 0.",
						"minimum": 0,
						"maximum": 5,
						"default": 0
					},
					"max_results": {
						"type": "integer",
						"description": "Maximum number of matches to return (1-500). Default: 100.",
						"minimum": 1,
						"maximum": 500,
						"default": 100
					}
				},
				"required": ["pattern"]
			}`),
		},
	}

	registry.Register(def, grepHandler(sandbox))
}

func grepHandler(sandbox *Sandbox) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args grepArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing grep arguments: %w", err)
		}

		if args.Pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}

		pat := args.Pattern
		if args.CaseInsensitive {
			pat = "(?i)" + pat
		}

		re, err := regexp.Compile(pat)
		if err != nil {
			return "", fmt.Errorf("invalid regex pattern %q: %w", args.Pattern, err)
		}

		searchPath := args.Path
		if searchPath == "" {
			searchPath = "."
		}

		maxResults := args.MaxResults
		if maxResults <= 0 {
			maxResults = grepDefaultMaxResults
		}
		if maxResults > grepMaxMaxResults {
			maxResults = grepMaxMaxResults
		}

		contextLines := args.ContextLines
		if contextLines < 0 {
			contextLines = 0
		}
		if contextLines > grepMaxContextLines {
			contextLines = grepMaxContextLines
		}

		abs, err := sandbox.Resolve(searchPath)
		if err != nil {
			return "", err
		}

		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("path not found: %s", args.Path)
			}
			return "", fmt.Errorf("accessing path: %w", err)
		}

		var filePaths []string
		var filesSkipped int

		if info.IsDir() {
			filePaths, filesSkipped = collectFiles(abs, args.Include, sandbox)
		} else {
			switch {
			case sandbox.IsSensitiveFile(abs):
				filesSkipped = 1
			case isBinaryFile(abs):
				filesSkipped = 1
			default:
				filePaths = []string{abs}
			}
		}

		gCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		type fileMatches struct {
			matches []grepMatch
		}

		allResults := make([]fileMatches, len(filePaths))
		var totalCount atomic.Int32
		sem := make(chan struct{}, grepConcurrency)
		var wg sync.WaitGroup

		for i, fp := range filePaths {
			wg.Add(1)
			go func(idx int, filePath string) {
				defer wg.Done()

				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-gCtx.Done():
					return
				}

				if int(totalCount.Load()) >= maxResults {
					return
				}

				relPath, relErr := filepath.Rel(sandbox.Root(), filePath)
				if relErr != nil {
					return
				}

				matches := searchFile(filePath, relPath, re, contextLines, maxResults, &totalCount)
				if len(matches) > 0 {
					allResults[idx] = fileMatches{matches: matches}
					if int(totalCount.Load()) >= maxResults {
						cancel()
					}
				}
			}(i, fp)
		}

		wg.Wait()

		var results []grepMatch
		var filesMatched int
		for _, fm := range allResults {
			if len(fm.matches) > 0 {
				filesMatched++
				results = append(results, fm.matches...)
			}
		}

		if results == nil {
			results = []grepMatch{}
		}

		sort.Slice(results, func(i, j int) bool {
			if results[i].File != results[j].File {
				return results[i].File < results[j].File
			}
			return results[i].Line < results[j].Line
		})

		truncated := len(results) > maxResults
		if truncated {
			results = results[:maxResults]
		}
		if !truncated {
			truncated = int(totalCount.Load()) >= maxResults && len(filePaths) > 0
		}

		res := grepResult{
			Pattern: args.Pattern,
			Path:    args.Path,
			Results: results,
			Summary: grepSummary{
				TotalMatches:  len(results),
				FilesSearched: len(filePaths),
				FilesMatched:  filesMatched,
				FilesSkipped:  filesSkipped,
				Truncated:     truncated,
			},
		}

		out, err := json.Marshal(res)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}

func collectFiles(root, include string, sandbox *Sandbox) (files []string, skipped int) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		if sandbox.IsSensitiveFile(path) {
			skipped++
			return nil
		}

		if include != "" {
			matched, matchErr := filepath.Match(include, d.Name())
			if matchErr != nil || !matched {
				return nil
			}
		}

		if isBinaryFile(path) {
			skipped++
			return nil
		}

		files = append(files, path)
		return nil
	})
	return files, skipped
}

func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, grepBinaryDetectSize)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}

	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

func searchFile(path, relPath string, re *regexp.Regexp, contextLines, maxResults int, totalCount *atomic.Int32) []grepMatch {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	var matches []grepMatch
	var lineNum int

	beforeBuf := make([]string, 0, contextLines)

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if re.MatchString(line) {
			if int(totalCount.Add(1)) > maxResults {
				break
			}

			var ctxBefore []string
			if contextLines > 0 {
				ctxBefore = make([]string, len(beforeBuf))
				copy(ctxBefore, beforeBuf)
			}

			match := grepMatch{
				File:          relPath,
				Line:          lineNum,
				Content:       line,
				ContextBefore: ctxBefore,
			}

			if contextLines > 0 {
				var ctxAfter []string
				for range contextLines {
					if !scanner.Scan() {
						break
					}
					lineNum++
					ctxAfter = append(ctxAfter, scanner.Text())
				}
				match.ContextAfter = ctxAfter
			}

			matches = append(matches, match)

			beforeBuf = beforeBuf[:0]
			continue
		}

		if contextLines > 0 {
			if len(beforeBuf) >= contextLines {
				beforeBuf = beforeBuf[1:]
			}
			beforeBuf = append(beforeBuf, line)
		}
	}

	return matches
}
