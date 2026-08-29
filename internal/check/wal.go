package check

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&walCheck{}) }

type walCheck struct{}

func (c *walCheck) Name() string { return "wal" }
func (c *walCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *walCheck) DefaultInterval() time.Duration { return 15 * time.Second }
func (c *walCheck) Timeout() time.Duration         { return 5 * time.Second }

func (c *walCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	columns, err := probeWALColumns(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	var result Result
	lsnRow := conn.QueryRow(ctx, `
SELECT lsn::text, pg_wal_lsn_diff(lsn, '0/0')::double precision
FROM (SELECT CASE WHEN pg_is_in_recovery()
                  THEN pg_last_wal_replay_lsn()
                  ELSE pg_current_wal_lsn()
             END AS lsn) wal_lsn`)
	var lsn *string
	var lsnBytes *float64
	if err := lsnRow.Scan(&lsn, &lsnBytes); err != nil {
		return Result{}, err
	}
	if lsnBytes != nil {
		result.Metrics = append(result.Metrics, pgtype.Metric{
			Name: "pg_wal_lsn_bytes", Value: *lsnBytes, Kind: pgtype.KindCounter,
		})
	}

	result.Metrics = append(result.Metrics, pgtype.Metric{
		Name: "pg_wal_stats_columns_available", Value: float64(len(columns)), Kind: pgtype.KindGauge,
	})
	if len(columns) == 0 {
		return result, nil
	}
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = quoteIdentifier(column) + "::text"
	}
	rows, err := conn.Query(ctx, "SELECT "+strings.Join(quoted, ", ")+" FROM pg_stat_wal")
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	if rows.Next() {
		values := make([]*string, len(columns))
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return Result{}, err
		}
		for i, column := range columns {
			if values[i] == nil {
				continue
			}
			value, err := strconv.ParseFloat(*values[i], 64)
			if err != nil {
				log.Printf("wal: skipping non-numeric pg_stat_wal column %q value %q", column, *values[i])
				continue
			}
			kind := pgtype.KindGauge
			if walCounterColumn(column) {
				kind = pgtype.KindCounter
			}
			result.Metrics = append(result.Metrics, pgtype.Metric{
				Name: "pg_wal_" + column, Value: value, Kind: kind,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	_ = lsn // The numeric offset is the stable metric; text is retained for scan compatibility.
	return result, nil
}

func probeWALColumns(ctx context.Context, conn Conn) ([]string, error) {
	rows, err := conn.Query(ctx, `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = 'pg_catalog' AND table_name = 'pg_stat_wal'
ORDER BY ordinal_position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func walCounterColumn(column string) bool {
	return strings.HasSuffix(column, "_count") || strings.HasSuffix(column, "_bytes") ||
		column == "wal_records" || column == "wal_fpi" || column == "wal_buffers_full" ||
		column == "wal_sync" || column == "wal_write"
}

func quoteIdentifier(identifier string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(identifier, `"`, `""`))
}
