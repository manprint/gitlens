package alert

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sil() Silence {
	return Silence{Matchers: []Matcher{{Name: "severity", Value: "warning"}}, StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}
}
func TestSilence_Validate_RejectsEmptyMatchers(t *testing.T) {
	s := sil()
	s.Matchers = nil
	require.EqualError(t, s.Validate(), "silence has no matchers")
}
func TestSilence_Validate_RejectsBadRegex(t *testing.T) {
	s := sil()
	s.Matchers = []Matcher{{Name: "x", Value: "[", IsRegex: true}}
	require.ErrorContains(t, s.Validate(), "invalid matcher regex")
}
func TestSilence_Validate_RejectsEmptyMatcherName(t *testing.T) {
	s := sil()
	s.Matchers = []Matcher{{Value: "x"}}
	require.EqualError(t, s.Validate(), "matcher name is empty")
}
func TestSilence_Validate_AcceptsRegex(t *testing.T) {
	s := sil()
	s.Matchers = []Matcher{{Name: "x", Value: "warn.*", IsRegex: true}}
	require.NoError(t, s.Validate())
}
func TestSilence_Validate_RejectsEndBeforeStart(t *testing.T) {
	s := sil()
	s.EndsAt = s.StartsAt.Add(-time.Second)
	require.EqualError(t, s.Validate(), "silence ends before it starts")
}
func TestSilence_Active_WindowIsHalfOpen(t *testing.T) {
	s := sil()
	require.True(t, s.Active(time.Unix(10, 0)))
	require.False(t, s.Active(time.Unix(20, 0)))
}
func TestSilence_Matches_AllMatchersMustMatch(t *testing.T) {
	s := sil()
	a := Alert{Severity: SeverityWarning, Labels: map[string]string{"x": "y"}}
	require.True(t, s.Matches(a))
	s.Matchers = append(s.Matchers, Matcher{Name: "x", Value: "z"})
	require.False(t, s.Matches(a))
}
func TestSilence_Matches_RegexIsAnchored(t *testing.T) {
	s := sil()
	s.Matchers = []Matcher{{Name: "x", Value: "warn", IsRegex: true}}
	require.False(t, s.Matches(Alert{Labels: map[string]string{"x": "warning"}}))
}
func TestSilence_Matches_NegateInverts(t *testing.T) {
	s := sil()
	s.Matchers = []Matcher{{Name: "x", Value: "bad", Negate: true}}
	require.True(t, s.Matches(Alert{Labels: map[string]string{"x": "good"}}))
}
func TestSilence_Matches_ReservedKeys(t *testing.T) {
	s := sil()
	a := Alert{RuleID: "r", Severity: SeverityWarning}
	s.Matchers = []Matcher{{Name: "rule_id", Value: "r"}, {Name: "severity", Value: "warning"}}
	require.True(t, s.Matches(a))
}
func TestFirstMatch_SkipsInactiveSilence(t *testing.T) {
	s := sil()
	a := Alert{Severity: SeverityWarning}
	require.Nil(t, FirstMatch([]Silence{s}, a, time.Unix(1, 0)))
	require.NotNil(t, FirstMatch([]Silence{s}, a, time.Unix(11, 0)))
}
func TestFirstMatch_ReturnsNilWhenNoneMatch(t *testing.T) {
	s := sil()
	require.Nil(t, FirstMatch([]Silence{s}, Alert{Severity: SeverityCritical}, time.Unix(11, 0)))
}
