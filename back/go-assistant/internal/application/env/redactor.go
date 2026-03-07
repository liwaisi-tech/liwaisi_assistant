// Package env provides secure environment variable management for the liwaisi agent.
package env

import (
	"sort"
	"strings"
	"sync"
)

// Redactor scans strings for known secret values and replaces them with
// safe placeholders. It is safe for concurrent use.
type Redactor struct {
	mu     sync.RWMutex
	values map[string]string // secret value -> env var key name
}

// NewRedactor creates an empty Redactor.
func NewRedactor() *Redactor {
	return &Redactor{
		values: make(map[string]string),
	}
}

// Register adds a secret value to the redaction set. If the same key is
// registered again the previous value mapping is removed first.
func (r *Redactor) Register(key, value string) {
	if value == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	for v, k := range r.values {
		if k == key {
			delete(r.values, v)
			break
		}
	}
	r.values[value] = key
}

// Unregister removes the secret value associated with key from the redaction set.
func (r *Redactor) Unregister(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for v, k := range r.values {
		if k == key {
			delete(r.values, v)
			return
		}
	}
}

// Redact replaces all occurrences of registered secret values in output with
// [REDACTED:KEY_NAME]. Longer values are replaced first to avoid partial matches.
func (r *Redactor) Redact(output string) string {
	r.mu.RLock()
	if len(r.values) == 0 {
		r.mu.RUnlock()
		return output
	}

	type entry struct {
		value string
		key   string
	}
	entries := make([]entry, 0, len(r.values))
	for v, k := range r.values {
		entries = append(entries, entry{value: v, key: k})
	}
	r.mu.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].value) > len(entries[j].value)
	})

	for _, e := range entries {
		output = strings.ReplaceAll(output, e.value, "[REDACTED:"+e.key+"]")
	}
	return output
}
