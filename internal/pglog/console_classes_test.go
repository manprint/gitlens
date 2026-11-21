package pglog

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// The console's class filter (web/src/lib/logs.ts LOG_CLASSES) lists exactly
// the shipped classes, in the order of Classes().
func TestConsoleListsShippedClasses(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "logs.ts"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)export const LOG_CLASSES = \[(.*?)\] as const`).FindSubmatch(body)
	if block == nil {
		t.Fatal("LOG_CLASSES not found in web/src/lib/logs.ts")
	}
	var console []string
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(block[1], -1) {
		console = append(console, string(m[1]))
	}
	var shipped []string
	for _, c := range Classes() {
		if Shipped(c) {
			shipped = append(shipped, c)
		}
	}
	if !slices.Equal(console, shipped) {
		t.Fatalf("LOG_CLASSES = %v, shipped classes = %v", console, shipped)
	}
}
