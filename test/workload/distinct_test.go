package main

import (
	"strings"
	"testing"
	"time"
)

// TestDistinct_ParseTreesDiffer verifies that generated statements are
// pairwise distinct after literal normalization. PostgreSQL normalizes
// literals in pg_stat_statements, so we must ensure our expressions have
// distinct parse trees.
func TestDistinct_ParseTreesDiffer(t *testing.T) {
	tests := []struct {
		index    int
		expected string
	}{
		{0, "1+1"},
		{1, "1+1+1"},
		{2, "1+1+1+1"},
		{3, "1+1+1+1+1"},
		{10, "1+1+1+1+1+1+1+1+1+1+1+1"},
	}

	for _, tt := range tests {
		t.Run(strings.Join(strings.Split(tt.expected, "+"), "-"), func(t *testing.T) {
			got := generateExpression(tt.index)
			if got != tt.expected {
				t.Errorf("generateExpression(%d) = %q, want %q", tt.index, got, tt.expected)
			}
		})
	}

	// Verify that all expressions up to 100 are unique
	t.Run("first_100_are_unique", func(t *testing.T) {
		seen := make(map[string]int)
		for i := 0; i < 100; i++ {
			expr := generateExpression(i)
			if prevIdx, exists := seen[expr]; exists {
				t.Errorf("duplicate expression at index %d (previously seen at %d): %q", i, prevIdx, expr)
			}
			seen[expr] = i
		}
	})

	// Verify that expressions truly have different parse tree structures
	// by checking they have different operator counts (though the actual
	// parse tree comparison would require a PostgreSQL connection)
	t.Run("parse_tree_structure_differs", func(t *testing.T) {
		// Each expression should have exactly (index+1) plus operators
		for i := 0; i < 20; i++ {
			expr := generateExpression(i)
			plusCount := strings.Count(expr, "+")
			expectedCount := i + 1
			if plusCount != expectedCount {
				t.Errorf("generateExpression(%d) has %d plus operators, want %d", i, plusCount, expectedCount)
			}
		}
	})
}

// TestDistinct_ParseTreesDiffer_FullWorkloadRange verifies that all 5000
// indices SYS-LOAD-008 actually drives (test/scenario/statements.go) stay
// unique and that no single "+" chain gets anywhere near PostgreSQL's
// max_stack_depth. A chain around ~4000-4200 terms starts failing with
// "stack depth limit exceeded" — confirmed live against a real instance,
// where the original unbounded-chain generateExpression silently dropped
// ~18% of a --count 5000 run's highest-index queries for exactly this
// reason.
func TestDistinct_ParseTreesDiffer_FullWorkloadRange(t *testing.T) {
	const workloadCount = 5000
	const safeChainBound = 1000 // well under the ~4000-4200 term failure threshold

	seen := make(map[string]int, workloadCount)
	for i := 0; i < workloadCount; i++ {
		expr := generateExpression(i)
		if prev, exists := seen[expr]; exists {
			t.Fatalf("duplicate expression at index %d (previously seen at %d): %q", i, prev, expr)
		}
		seen[expr] = i

		for _, col := range strings.Split(expr, ",") {
			if depth := strings.Count(col, "+"); depth > safeChainBound {
				t.Errorf("generateExpression(%d) column %q has chain depth %d, exceeds safe bound %d", i, col, depth, safeChainBound)
			}
		}
	}
}

func TestDistinct_RotationCountHonorsDurationWindow(t *testing.T) {
	const groups = 100
	for _, tc := range []struct {
		name     string
		duration time.Duration
		want     int
	}{
		{name: "zero", duration: 0, want: 0},
		{name: "one_period", duration: rotationPeriod, want: 1},
		{name: "six_periods", duration: 390 * time.Second, want: 6},
		{name: "partial_seventh", duration: 391 * time.Second, want: 7},
		{name: "capped_at_groups", duration: 1000 * time.Hour, want: groups},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rotationCount(tc.duration, groups); got != tc.want {
				t.Fatalf("rotationCount(%s, %d) = %d, want %d", tc.duration, groups, got, tc.want)
			}
		})
	}
}
