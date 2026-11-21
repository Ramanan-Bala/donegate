package rules

import (
	"fmt"
	"sort"
	"sync"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/runner"
)

// Severity controls how a rule's findings affect the gate verdict.
type Severity string

const (
	Off   Severity = "off"
	Warn  Severity = "warn"
	Block Severity = "block"
)

// Finding is a single rule violation detected on a diff.
type Finding struct {
	RuleID   string   `json:"rule"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
	Hint     string   `json:"hint,omitempty"`
}

// Tamper describes a protected file or policy snapshot whose hash differs from the session baseline.
type Tamper struct {
	Path     string
	Expected string
	Actual   string
}

// Context provides the diff and policy inputs to a rule check.
type Context struct {
	Diff         *diff.Diff
	Policy       *policy.Policy
	Tampered     []Tamper
	Runs         []runner.Result
	BaselineRuns []runner.Result
}

// Rule defines a single verification check.
type Rule interface {
	ID() string
	Level() int
	DefaultSeverity() Severity
	Check(c *Context) []Finding
}

var (
	mu       sync.RWMutex
	registry = map[string]Rule{}
)

// Register adds r to the global rule registry. It panics on nil, empty ID, or duplicate ID.
func Register(r Rule) {
	if r == nil {
		panic("rules: cannot register nil rule")
	}
	id := r.ID()
	if id == "" {
		panic("rules: cannot register rule with empty ID")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[id]; exists {
		panic(fmt.Sprintf("rules: duplicate rule ID %q", id))
	}
	registry[id] = r
}

// All returns every registered rule sorted by ID.
func All() []Rule {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Rule, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID() < out[j].ID()
	})
	return out
}

// Lookup returns the registered rule for id, if present.
func Lookup(id string) (Rule, bool) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := registry[id]
	return r, ok
}
