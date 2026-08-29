package alert

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validRule() Rule {
	return Rule{ID: "r", Severity: SeverityWarning, Scope: ScopeInstance, Metric: "m", Comparator: GT, For: time.Second}
}
func TestRule_Validate_RejectsEmptyID(t *testing.T) {
	r := validRule()
	r.ID = ""
	require.EqualError(t, r.Validate(), "rule id is empty")
}
func TestRule_Validate_RejectsUnknownSeverity(t *testing.T) {
	r := validRule()
	r.Severity = "x"
	require.EqualError(t, r.Validate(), `unknown severity "x"`)
}
func TestRule_Validate_RejectsUnknownScope(t *testing.T) {
	r := validRule()
	r.Scope = "x"
	require.EqualError(t, r.Validate(), `unknown scope "x"`)
}
func TestRule_Validate_RejectsNeitherMetricNorEvent(t *testing.T) {
	r := validRule()
	r.Metric = ""
	require.EqualError(t, r.Validate(), "rule has neither metric nor event type")
}
func TestRule_Validate_RejectsBothMetricAndEvent(t *testing.T) {
	r := validRule()
	r.EventType = "e"
	require.EqualError(t, r.Validate(), "rule has both metric and event type")
}
func TestRule_Validate_RejectsUnknownComparator(t *testing.T) {
	r := validRule()
	r.Comparator = "x"
	require.EqualError(t, r.Validate(), `unknown comparator "x"`)
}
func TestRule_Validate_RejectsNegativeFor(t *testing.T) {
	r := validRule()
	r.For = -time.Second
	require.EqualError(t, r.Validate(), "rule for duration is negative")
}
func TestRule_Validate_AcceptsMetricRule(t *testing.T) { require.NoError(t, validRule().Validate()) }
func TestRule_Validate_AcceptsEventRule(t *testing.T) {
	r := validRule()
	r.Metric = ""
	r.EventType = "e"
	require.NoError(t, r.Validate())
	require.True(t, strings.HasPrefix(r.EventType, "e"))
}
