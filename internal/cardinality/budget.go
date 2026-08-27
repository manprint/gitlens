package cardinality

// Budget caps the number of series admitted per instance per push.
type Budget struct {
	max int
}

// NewBudget creates a Budget with the given max.
func NewBudget(max int) *Budget {
	if max == 0 {
		max = 20000
	}
	return &Budget{max: max}
}

// Admit reports how many of n series fit and whether anything was refused.
func (b *Budget) Admit(used, n int) (admitted int, truncated bool) {
	if n == 0 {
		return 0, false
	}
	if used >= b.max {
		return 0, true
	}
	remaining := b.max - used
	if n <= remaining {
		return n, false
	}
	return remaining, true
}
