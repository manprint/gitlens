package advisor

import (
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const (
	functionTrackingFactKind = "function_tracking"
	securityDefinerFactKind  = "security_definer_function"
)

func init() {
	Register(relationRule{id: "function.secdef_no_search_path", severity: SeverityCritical, needs: []string{"function_security"}, eval: func(s *Snapshot) []Finding {
		const id = "function.secdef_no_search_path"
		var out []Finding
		for _, f := range currentFacts(s, securityDefinerFactKind) {
			if f.ValueText != "unpinned" {
				continue
			}
			l := f.Labels
			kind := l["kind"]
			if kind != "procedure" {
				kind = "function"
			}
			superuser, public := l["owner_superuser"] == "true", l["public_execute"] == "true"
			severity, owner := SeverityWarning, "role "+l["owner"]
			if superuser {
				owner = "superuser " + l["owner"]
				if public {
					severity = SeverityCritical
				}
			}
			callers := "Only the roles granted EXECUTE can call it."
			revoke := ""
			if public {
				callers = "Every role can call it: PUBLIC has EXECUTE, as it does on any function by default."
				revoke = fmt.Sprintf(" Then REVOKE EXECUTE ON %s %s FROM PUBLIC; and grant EXECUTE to the roles that need it.", upperKind(kind), f.Key)
			}
			out = append(out, relationFinding(id, severity, f.Datname, f.Key,
				fmt.Sprintf("SECURITY DEFINER %s %s does not pin search_path", kind, f.Key),
				fmt.Sprintf("%s in database %s runs with the privileges of its owner, %s, but finds the tables, functions and operators its body names through the caller's search_path. A caller who can create objects in a schema on that path, or a temporary table, can make it use their objects with those privileges. %s", f.Key, f.Datname, owner, callers),
				fmt.Sprintf("ALTER %s %s SET search_path = %s, pg_temp; with pg_temp last, and schema-qualify the objects its body uses.%s", upperKind(kind), f.Key, pgx.Identifier{l["schemaname"]}.Sanitize(), revoke),
				map[string]any{"function": f.Key, "owner": l["owner"], "owner_superuser": superuser, "public_execute": public, "language": l["language"]}))
		}
		return out
	}})
	Register(databaseRule{relationRule{id: "function.untracked", severity: SeverityInfo, needs: []string{"function_stats"}, eval: func(s *Snapshot) []Finding {
		const id = "function.untracked"
		var out []Finding
		for _, f := range currentFacts(s, functionTrackingFactKind) {
			pl, _ := strconv.Atoi(f.Labels["pl_functions"])
			// Another session or role may track although this one does not:
			// then the view has rows, and nothing is missing.
			if f.ValueText != "none" || f.Labels["tracked"] != "0" || pl == 0 {
				continue
			}
			out = append(out, Finding{RuleID: id, Severity: SeverityInfo, State: StateOpen, Scope: ScopeDatabase, Datname: f.Datname, ObjectName: "functions",
				Title:       fmt.Sprintf("Function calls are not tracked in database %s", f.Datname),
				Detail:      fmt.Sprintf("track_functions is none for the sessions of database %s, so pg_stat_user_functions is empty: how often its %s in a procedural language (trigger functions among them) run, and how long they take, is unknown.", f.Datname, plural(pl, "function")),
				Remediation: fmt.Sprintf("ALTER SYSTEM SET track_functions = 'pl'; SELECT pg_reload_conf(); or, for this database only, ALTER DATABASE %s SET track_functions = 'pl';. 'pl' costs little; 'all' also tracks SQL and C functions at a higher cost, and an SQL function the planner inlines is never tracked. pglens never changes the setting.", pgx.Identifier{f.Datname}.Sanitize()),
				Evidence:    map[string]any{"track_functions": f.ValueText, "pl_functions": pl},
			})
		}
		return out
	}}})
}

func upperKind(kind string) string {
	if kind == "procedure" {
		return "PROCEDURE"
	}
	return "FUNCTION"
}
