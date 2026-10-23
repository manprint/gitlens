package advisor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func securityDefinerFact(datname, key, state string, observed time.Time, extra map[string]string) Fact {
	labels := map[string]string{
		"schemaname": "public", "name": "f", "arguments": "", "kind": "function", "language": "plpgsql",
		"owner": "app", "owner_superuser": "false", "public_execute": "false", "search_path": "", "pg_temp_last": "false",
	}
	for k, v := range extra {
		labels[k] = v
	}
	return Fact{Key: key, Datname: datname, ValueText: state, Labels: labels, ObservedAt: observed}
}

func TestFunctionSecdefNoSearchPathRule(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "function.secdef_no_search_path")
	s := catalogSnapshot(now, securityDefinerFactKind,
		securityDefinerFact("app", "public.grant_all(r text)", "unpinned", now, map[string]string{
			"name": "grant_all", "owner": "postgres", "owner_superuser": "true", "public_execute": "true"}),
		securityDefinerFact("app", `"My Schema".job()`, "unpinned", now, map[string]string{
			"schemaname": "My Schema", "name": "job", "kind": "procedure"}),
		securityDefinerFact("app", "public.safe()", "pinned", now, map[string]string{"search_path": "public, pg_temp"}),
	)
	assertRuleContract(t, rule, s)
	got := rule.Evaluate(s)
	require.Len(t, got, 2)
	byObject := map[string]Finding{}
	for _, f := range got {
		byObject[f.ObjectName] = f
	}

	exposed := byObject["public.grant_all(r text)"]
	require.Equal(t, SeverityCritical, exposed.Severity)
	require.Equal(t, "SECURITY DEFINER function public.grant_all(r text) does not pin search_path", exposed.Title)
	require.Contains(t, exposed.Detail, "its owner, superuser postgres")
	require.Contains(t, exposed.Detail, "Every role can call it")
	require.Equal(t, `ALTER FUNCTION public.grant_all(r text) SET search_path = "public", pg_temp; with pg_temp last, and schema-qualify the objects its body uses. Then REVOKE EXECUTE ON FUNCTION public.grant_all(r text) FROM PUBLIC; and grant EXECUTE to the roles that need it.`, exposed.Remediation)

	job := byObject[`"My Schema".job()`]
	require.Equal(t, SeverityWarning, job.Severity)
	require.Contains(t, job.Detail, "Only the roles granted EXECUTE")
	require.Equal(t, `ALTER PROCEDURE "My Schema".job() SET search_path = "My Schema", pg_temp; with pg_temp last, and schema-qualify the objects its body uses.`, job.Remediation)
}

func TestFunctionUntrackedRule(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "function.untracked")
	tracking := func(datname, setting, tracked, pl string) Fact {
		return Fact{Key: "track_functions", Datname: datname, ValueText: setting,
			Labels: map[string]string{"tracked": tracked, "pl_functions": pl}, ObservedAt: now}
	}
	s := catalogSnapshot(now, functionTrackingFactKind,
		tracking("app", "none", "0", "12"),
		// Tracked by other sessions, no functions, or already tracking.
		tracking("crm", "none", "4", "12"),
		tracking("empty", "none", "0", "0"),
		tracking("on", "pl", "0", "3"),
	)
	assertRuleContract(t, rule, s)
	got := rule.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, "app", got[0].Datname)
	require.Equal(t, ScopeDatabase, got[0].Scope)
	require.Equal(t, "Function calls are not tracked in database app", got[0].Title)
	require.Contains(t, got[0].Detail, "its 12 functions in a procedural language")
	require.Contains(t, got[0].Remediation, `ALTER DATABASE "app" SET track_functions = 'pl';`)
}
