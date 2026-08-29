package advisor

import (
	"sort"
	"sync"
)

var (
	registryMu sync.Mutex
	registry   = map[string]Rule{}
)

func Register(r Rule) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[r.ID()]; exists {
		panic("advisor rule already registered: " + r.ID())
	}
	registry[r.ID()] = r
}

func All() []Rule {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]Rule, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

func ResetForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]Rule{}
}
