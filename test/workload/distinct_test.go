package main

import (
	"strings"
	"testing"
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
