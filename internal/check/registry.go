package check

import (
	"sort"
	"sync"
)

var (
	mu       sync.Mutex
	registry = map[string]Check{}
)

// Register panics on a duplicate name. Registration happens in package init
// functions, so a duplicate is a programming error that must fail at startup.
func Register(c Check) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := registry[c.Name()]; ok {
		panic("check already registered: " + c.Name())
	}
	registry[c.Name()] = c
}

// Get returns a check by name.
func Get(name string) (Check, bool) {
	mu.Lock()
	defer mu.Unlock()
	c, ok := registry[name]
	return c, ok
}

// All returns all checks sorted by name, so iteration order is deterministic.
func All() []Check {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Check, 0, len(registry))
	for _, c := range registry {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

// ResetForTest clears the registry for tests.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	registry = map[string]Check{}
}
