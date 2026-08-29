//go:build integration

package advisor

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTADV007_SeededRulePackHasExactSet(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		seed, err := os.ReadFile("../../test/fixtures/sql/advisor_seed.sql")
		require.NoError(t, err)
		_, err = pool.Exec(context.Background(), string(seed))
		require.NoError(t, err)

		now := time.Now().UTC()
		id := uuid.New()
		s := advisorIntegrationSnapshot(id, now, 0)
		s.Statements = []StatementStat{{QueryID: 17, MeanExecTimeMs: 700, Calls: 20, TotalExecTimeMs: 14000}}
		s.Baseline = &Baseline{MeanExecTimeByQueryID: map[int64]float64{17: 100}}
		s.Tables = []TableStat{
			{Name: "public.advisor_dead", NLiveTup: 10000, NDeadTup: 15000, NModSinceAnalyze: 0},
			{Name: "public.advisor_never_vacuumed", NLiveTup: 100001},
		}
		s.Indexes = []IndexStat{
			{Name: "advisor_unused_large_idx", TableName: "advisor_unused", SizeBytes: 11 * 1024 * 1024, HistoryDays: 7, IsValid: true, IndexDef: "CREATE INDEX advisor_unused_large_idx ON advisor_unused (payload)"},
			{Name: "advisor_prefix_a", TableName: "advisor_prefix", IsValid: true, IndexDef: "CREATE INDEX advisor_prefix_a ON advisor_prefix (a)"},
			{Name: "advisor_prefix_ab", TableName: "advisor_prefix", IsValid: true, IndexDef: "CREATE INDEX advisor_prefix_ab ON advisor_prefix (a, b)"},
		}
		s.Bloat = []BloatStat{{Name: "advisor_bloated", RelationName: "advisor_bloated", Ratio: .5, SizeBytes: 101 * 1024 * 1024}}
		s.Host.TotalBytes = 16 * 1024 * 1024 * 1024
		s.Settings["shared_buffers_bytes"] = "3221225472"
		s.Metrics["pg_xact_rollback_ratio"][""] = .2
		s.Metrics["pg_max_xact_age_seconds"][""] = 0

		got := map[string]bool{}
		for _, rule := range All() {
			for _, finding := range rule.Evaluate(s) {
				if finding.State == StateOpen {
					got[finding.RuleID] = true
				}
			}
		}
		ids := make([]string, 0, len(got))
		for id := range got {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		require.Equal(t, []string{"index.bloat_high", "index.redundant_prefix", "index.unused", "query.regression", "query.slow_mean", "query.total_time_share", "table.bloat_high", "table.dead_tuples_high", "table.never_autovacuumed", "txn.high_rollback_ratio"}, ids)
	})
}

func TestINTADV008_T0MissingBloatInputDegrades(t *testing.T) {
	s := advisorIntegrationSnapshot(uuid.New(), time.Now(), 0)
	s.Bloat = nil
	for _, rule := range All() {
		if rule.ID() == "index.bloat_high" || rule.ID() == "table.bloat_high" {
			findings, _ := (&Engine{}).evaluateRule(rule, s)
			require.NotEmpty(t, findings)
		}
	}
}
