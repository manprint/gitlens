package check

import (
	"sync"

	"github.com/manprint/pglens/internal/cardinality"
)

type RelKind string

const (
	RelKindTable RelKind = "table"
	RelKindIndex RelKind = "index"
	RelKindBloat RelKind = "bloat"
)

type RelSelector struct {
	mu        sync.Mutex
	selectors map[RelKind]*cardinality.Selector
	max       map[RelKind]int
}

func NewRelSelector(maxTables, maxIndexes int) *RelSelector {
	if maxTables <= 0 {
		maxTables = 200
	}
	if maxIndexes <= 0 {
		maxIndexes = 200
	}
	return &RelSelector{selectors: map[RelKind]*cardinality.Selector{RelKindTable: cardinality.NewSelector(cardinality.Options{TopN: maxTables + 1, MaxKeys: maxTables}), RelKindIndex: cardinality.NewSelector(cardinality.Options{TopN: maxIndexes + 1, MaxKeys: maxIndexes}), RelKindBloat: cardinality.NewSelector(cardinality.Options{TopN: maxTables + 1, MaxKeys: maxTables})}, max: map[RelKind]int{RelKindTable: maxTables, RelKindIndex: maxIndexes, RelKindBloat: maxTables}}
}
func (s *RelSelector) Select(kind RelKind, cycle uint64, in []cardinality.Candidate) ([]cardinality.Candidate, bool) {
	s.mu.Lock()
	sel := s.selectors[kind]
	s.mu.Unlock()
	if sel == nil {
		return nil, false
	}
	return sel.Select(cycle, in)
}
