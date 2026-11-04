package readiness

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var docRuleID = regexp.MustCompile(`(?m)^### (RDY-[A-Z0-9]+-[0-9]+)`)

// RDY-DOC-001: docs/MONITORING_READINESS.md has a section for every rule id and
// no section for an id without a rule.
func TestRDYDOC001_DocumentationListsEveryRule(t *testing.T) {
	raw, err := os.ReadFile("../../docs/MONITORING_READINESS.md")
	require.NoError(t, err)
	documented := map[string]bool{}
	for _, m := range docRuleID.FindAllStringSubmatch(string(raw), -1) {
		require.False(t, documented[m[1]], "%s is documented twice", m[1])
		documented[m[1]] = true
	}
	have := map[string]bool{}
	for _, rule := range Rules() {
		have[rule.ID] = true
		require.True(t, documented[rule.ID], "%s has no section in docs/MONITORING_READINESS.md", rule.ID)
	}
	for id := range documented {
		require.True(t, have[id], "%s is documented but has no rule", id)
	}
}

// RDY-DOC-002: every section heading has the form "### <id> — <title>".
func TestRDYDOC002_DocumentedTitlesMatch(t *testing.T) {
	raw, err := os.ReadFile("../../docs/MONITORING_READINESS.md")
	require.NoError(t, err)
	doc := string(raw)
	for _, rule := range Rules() {
		require.Contains(t, doc, "### "+rule.ID+" — ")
	}
}
