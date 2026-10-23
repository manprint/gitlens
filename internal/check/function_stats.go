package check

import (
	"context"
	"strconv"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&functionStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})})
}

// functionTrackingFactKind is the fact kind of a database's function
// tracking: the track_functions value its sessions run with, how many
// functions the statistics view holds, and how many procedural-language
// functions the database defines. Without tracking the view is empty, which
// is not the same as no function being called.
const functionTrackingFactKind = "function_tracking"

// functionTrackingQuery reads track_functions as this session sees it, which
// is the value for the database (an ALTER DATABASE ... SET applies), and
// counts the functions written in a procedural language, outside extensions:
// the ones track_functions = 'pl' would track.
const functionTrackingQuery = `SELECT pg_catalog.current_setting('track_functions'),
       (SELECT pg_catalog.count(*)
          FROM pg_catalog.pg_proc p
          JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
          JOIN pg_catalog.pg_language l ON l.oid = p.prolang
         WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
           AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
           AND l.lanname NOT IN ('internal', 'c', 'sql')
           AND p.prokind IN ('f', 'p')
           AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend d
                            WHERE d.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
                              AND d.objid = p.oid AND d.deptype = 'e'))`

// functionStatsQuery reads the cumulative call counters of every tracked
// function. The identity arguments tell overloads apart: two functions of
// one name would otherwise be one series.
const functionStatsQuery = `SELECT s.funcid, s.schemaname, s.funcname,
       COALESCE(pg_catalog.pg_get_function_identity_arguments(s.funcid), ''),
       COALESCE(l.lanname, ''), s.calls, s.total_time, s.self_time
  FROM pg_catalog.pg_stat_user_functions s
  LEFT JOIN pg_catalog.pg_proc p ON p.oid = s.funcid
  LEFT JOIN pg_catalog.pg_language l ON l.oid = p.prolang`

// functionStatsCheck reports how often each tracked function runs and how
// long it takes, for the functions with the most time spent in their own
// body (self time: total time counts the functions a function calls once
// more) and the most calls. The server turns the counters into rates.
type functionStatsCheck struct {
	selectors *scopedSelectors
}

type functionStatsRow struct {
	oid                      uint32
	schema, name, args, lang string
	calls                    int64
	totalTimeMs, selfTimeMs  float64
}

func (c *functionStatsCheck) Name() string { return "function_stats" }
func (c *functionStatsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *functionStatsCheck) DefaultInterval() time.Duration { return 5 * time.Minute }
func (c *functionStatsCheck) Timeout() time.Duration         { return 15 * time.Second }
func (c *functionStatsCheck) SetTopN(topN int)               { c.selectors.SetTopN(topN) }

func (c *functionStatsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	var setting string
	var plFunctions int64
	if err := conn.QueryRow(ctx, functionTrackingQuery).Scan(&setting, &plFunctions); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, functionStatsQuery)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var parsed []functionStatsRow
	for rows.Next() {
		var r functionStatsRow
		if err := rows.Scan(&r.oid, &r.schema, &r.name, &r.args, &r.lang, &r.calls, &r.totalTimeMs, &r.selfTimeMs); err != nil {
			return Result{}, err
		}
		parsed = append(parsed, r)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	selector, cycle := c.selectors.next(t)
	candidates := make([]cardinality.Candidate, 0, len(parsed))
	byKey := make(map[string]functionStatsRow, len(parsed))
	for _, r := range parsed {
		key := strconv.FormatUint(uint64(r.oid), 10)
		byKey[key] = r
		candidates = append(candidates, cardinality.Candidate{Key: key, Primary: r.selfTimeMs, Secondary: float64(r.calls)})
	}
	selected, truncated := selector.Select(cycle, candidates)
	selector.Forget(cycle)

	var result Result
	for _, candidate := range selected {
		r, ok := byKey[candidate.Key]
		if !ok {
			continue
		}
		labels := map[string]string{"schemaname": r.schema, "function": r.name, "arguments": r.args, "language": r.lang}
		result.Metrics = append(result.Metrics,
			pgtype.Metric{Name: "pg_function_calls_total", Labels: labels, Value: float64(r.calls), Kind: pgtype.KindCounter},
			pgtype.Metric{Name: "pg_function_total_time_ms_total", Labels: labels, Value: r.totalTimeMs, Kind: pgtype.KindCounter},
			pgtype.Metric{Name: "pg_function_self_time_ms_total", Labels: labels, Value: r.selfTimeMs, Kind: pgtype.KindCounter},
		)
	}
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_functions_tracked", Value: float64(len(parsed)), Kind: pgtype.KindGauge})
	result.Facts = append(result.Facts, Fact{
		Kind: functionTrackingFactKind, Key: "track_functions", ValueText: setting,
		Labels: map[string]string{
			"tracked":      strconv.Itoa(len(parsed)),
			"pl_functions": strconv.FormatInt(plFunctions, 10),
		},
	})
	// As in table_stats: Truncated is the hard cap, and the top-N subset
	// of a database with more tracked functions is the normal case.
	result.Truncated = truncated
	result.Partial = len(selected) < len(candidates)
	return result, nil
}
