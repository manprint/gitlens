package command

import "github.com/manprint/pglens/internal/pgtype"

// Allowed reports whether the command may run, and why not when it may not.
// The reason is returned to the caller verbatim, so it names the closed gate
// without exposing unrelated configuration.
func Allowed(k Kind, a Args, g Gates) (bool, string) {
	switch k {
	case KindExplain:
		if g.Tier < pgtype.TierExplain {
			return false, "permission tier T1 is required"
		}
		if a.Analyze && !g.AllowExplainAnalyze {
			return false, "EXPLAIN ANALYZE gate is closed"
		}
		return true, ""
	case KindCancel, KindTerminate:
		if g.Tier < pgtype.TierSignal {
			return false, "permission tier T2 is required"
		}
		if !g.AllowSignal {
			return false, "signal gate is closed"
		}
		return true, ""
	case KindPgstattuple:
		if g.Tier < pgtype.TierExplain {
			return false, "permission tier T1 is required"
		}
		if !g.HasPgstattuple {
			return false, "pgstattuple extension gate is closed"
		}
		return true, ""
	default:
		return false, "unknown command kind"
	}
}
