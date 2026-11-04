// Package sessionorigin tells the sessions pglens opens itself from the ones
// the application opens. It is stdlib only, so the agent, the checks and the
// server share one definition.
package sessionorigin

import (
	"strings"
)

const (
	// Pglens and Application are the two origins.
	Pglens      = "pglens"
	Application = "application"
)

// Prefixes are the application_name prefixes of pglens sessions. The name
// "pglens" alone (the monitoring role's default) also counts.
var Prefixes = []string{"pglens/", "pglens-agent/"}

// IsPglensApplication reports whether an application_name belongs to a pglens
// session.
func IsPglensApplication(name string) bool {
	if name == Pglens {
		return true
	}
	for _, p := range Prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Origin classifies a session: pglens when its application_name is one of
// pglens's or it runs under the monitoring role, application otherwise. An
// empty monitorRole never matches (a session with no usename included).
func Origin(applicationName, usename, monitorRole string) string {
	if IsPglensApplication(applicationName) || (monitorRole != "" && usename == monitorRole) {
		return Pglens
	}
	return Application
}

// ApplicationSQL renders the SQL predicate "this application_name is a pglens
// one" over appExpr, an expression of the application name (NULL counts as
// empty). Its answer is IsPglensApplication's.
func ApplicationSQL(appExpr string) string {
	name := "COALESCE(" + appExpr + ", '')"
	parts := []string{name + " = '" + Pglens + "'"}
	for _, p := range Prefixes {
		parts = append(parts, "starts_with("+name+", '"+p+"')")
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// PredicateSQL renders the SQL predicate "this session is a pglens session":
// ApplicationSQL over appExpr, or the session's user is the one the query runs
// as (userExpr = current_user), so targets with different DSN users stay
// correct. A NULL user never matches.
func PredicateSQL(appExpr, userExpr string) string {
	return "(" + ApplicationSQL(appExpr) + " OR COALESCE(" + userExpr + " = current_user, false))"
}
