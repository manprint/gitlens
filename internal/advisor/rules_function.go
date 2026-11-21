package advisor

import (
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/i18n"
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
			severity := SeverityWarning
			if superuser && public {
				severity = SeverityCritical
			}
			out = append(out, relationFinding(id, severity, f.Datname, f.Key, "", "", "",
				map[string]any{"function": f.Key, "owner": l["owner"], "owner_superuser": superuser, "public_execute": public, "language": l["language"]}).
				say("function.secdef_no_search_path", i18n.Args{"kind": kind, "function": f.Key, "database": f.Datname, "owner": l["owner"], "superuser": superuser, "public": public, "schema": pgx.Identifier{l["schemaname"]}.Sanitize()}))
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
				Evidence: map[string]any{"track_functions": f.ValueText, "pl_functions": pl},
			}.say("function.untracked", i18n.Args{"database": f.Datname, "count": pl, "databaseIdent": pgx.Identifier{f.Datname}.Sanitize()}))
		}
		return out
	}}})
}
