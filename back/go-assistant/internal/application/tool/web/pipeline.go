package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	readability "github.com/go-shiori/go-readability"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
)

// FetchResult holds the structured output of a web fetch operation.
type FetchResult struct {
	URL         string `json:"url"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type"`
	Title       string `json:"title"`
	Markdown    string `json:"markdown"`
	Byline      string `json:"byline,omitempty"`
	Excerpt     string `json:"excerpt,omitempty"`
	WordCount   int    `json:"word_count"`
	Truncated   bool   `json:"truncated"`
	Error       string `json:"error,omitempty"`
}

// Pipeline orchestrates URL fetching, content extraction, and Markdown conversion.
type Pipeline struct {
	client *http.Client
	cfg    *Config
}

// NewPipeline creates a Pipeline with the given configuration.
// It configures an HTTP client with SSRF-safe transport and the specified timeout.
func NewPipeline(cfg *Config) *Pipeline {
	return &Pipeline{
		client: &http.Client{
			Transport: NewSafeTransport(cfg.Timeout),
			Timeout:   cfg.Timeout,
		},
		cfg: cfg,
	}
}

// Fetch retrieves the content at rawURL and returns it as clean Markdown.
// When raw is false (default), Mozilla's Readability algorithm extracts the
// main article content. When raw is true, the full body is converted after
// stripping script/style/nav tags.
//
// Error contract: a non-nil Go error indicates invalid input (bad URL, request
// creation failure) and aborts the tool call. HTTP, network, and conversion
// errors are reported via FetchResult.Error so the LLM can self-correct.
func (p *Pipeline) Fetch(ctx context.Context, rawURL string, raw bool) (*FetchResult, error) {
	parsedURL, err := validateURL(rawURL)
	if err != nil {
		return nil, err
	}

	result := &FetchResult{URL: parsedURL.String()}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", p.cfg.UserAgent)
	req.Header.Set("Accept", p.cfg.Accept)

	resp, err := p.client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("fetch failed: %v", err)
		return result, nil
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.ContentType = resp.Header.Get("Content-Type")

	body, err := io.ReadAll(io.LimitReader(resp.Body, p.cfg.MaxBodySize))
	if err != nil {
		result.Error = fmt.Sprintf("reading response body: %v", err)
		return result, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		return result, nil
	}

	if !isHTMLContentType(result.ContentType) {
		result.Markdown = string(body)
		result.WordCount = countWords(result.Markdown)
		p.applyTruncation(result)
		return result, nil
	}

	domain := parsedURL.Scheme + "://" + parsedURL.Host

	if raw {
		md, err := convertRawHTML(string(body), domain)
		if err != nil {
			result.Error = fmt.Sprintf("converting HTML to markdown: %v", err)
			return result, nil
		}
		result.Markdown = md
	} else {
		md, title, byline, excerpt, err := extractWithReadability(body, parsedURL, domain)
		if err != nil {
			result.Error = fmt.Sprintf("extracting content: %v", err)
			return result, nil
		}
		result.Markdown = md
		result.Title = title
		result.Byline = byline
		result.Excerpt = excerpt
	}

	result.WordCount = countWords(result.Markdown)
	p.applyTruncation(result)

	return result, nil
}

// extractWithReadability uses Mozilla's Readability algorithm to pull the main
// article content, then converts the extracted HTML to Markdown.
func extractWithReadability(body []byte, pageURL *url.URL, domain string) (markdown, title, byline, excerpt string, err error) {
	article, err := readability.FromReader(bytes.NewReader(body), pageURL)
	if err != nil {
		return "", "", "", "", fmt.Errorf("readability extraction: %w", err)
	}

	md, err := htmlToMarkdown(article.Content, domain)
	if err != nil {
		return "", "", "", "", fmt.Errorf("html-to-markdown: %w", err)
	}

	return md, article.Title, article.Byline, article.Excerpt, nil
}

// convertRawHTML converts the full HTML body to Markdown, keeping all
// structural content (useful for index/listing pages).
func convertRawHTML(html, domain string) (string, error) {
	md, err := htmlToMarkdown(html, domain)
	if err != nil {
		return "", fmt.Errorf("html-to-markdown: %w", err)
	}
	return md, nil
}

// htmlToMarkdown converts an HTML string to Markdown using html-to-markdown v2.
// The domain parameter is used to resolve relative URLs to absolute.
func htmlToMarkdown(html, domain string) (string, error) {
	md, err := htmltomd.ConvertString(html, converter.WithDomain(domain))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(md), nil
}

// isHTMLContentType reports whether the Content-Type indicates HTML.
func isHTMLContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mediaType, _, _ := mime.ParseMediaType(ct)
	switch mediaType {
	case "text/html", "application/xhtml+xml":
		return true
	}
	return false
}

// applyTruncation truncates result.Markdown if it exceeds MaxContentLength.
func (p *Pipeline) applyTruncation(result *FetchResult) {
	if p.cfg.MaxContentLength > 0 && utf8.RuneCountInString(result.Markdown) > p.cfg.MaxContentLength {
		runes := []rune(result.Markdown)
		result.Markdown = string(runes[:p.cfg.MaxContentLength]) + "\n\n[Content truncated...]"
		result.Truncated = true
		result.WordCount = countWords(result.Markdown)
	}
}

// countWords counts whitespace-delimited words in s.
func countWords(s string) int {
	return len(strings.Fields(s))
}
