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

func init() { Register(&ioCheck{}) }

type ioCheck struct{}

func (c *ioCheck) Name() string { return "io" }
func (c *ioCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly, MinPG: pgtype.PG16}
}
func (c *ioCheck) DefaultInterval() time.Duration { return 30 * time.Second }
func (c *ioCheck) Timeout() time.Duration         { return 5 * time.Second }

var ioOptionalColumns = []string{
	"backend_type", "object", "context", "reads", "read_bytes", "writes", "write_bytes",
	"extends", "hits", "evictions", "fsyncs", "read_time", "write_time", "fsync_time",
}

func (c *ioCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	available, err := probeIOColumns(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	columns := make([]string, 0, len(ioOptionalColumns))
	for _, column := range ioOptionalColumns {
		if available[column] {
			columns = append(columns, column)
		}
	}
	var result Result
	var timingEnabled bool
	if err := conn.QueryRow(ctx, "SELECT current_setting('track_io_timing')::bool").Scan(&timingEnabled); err != nil {
		return Result{}, err
	}
	value := 0.0
	if timingEnabled {
		value = 1
	}
	result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_io_timing_enabled", Value: value, Kind: pgtype.KindGauge})
	if len(columns) < 3 {
		return result, nil
	}
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = quoteIdentifier(column) + "::text"
	}
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT %s FROM pg_stat_io WHERE reads > 0 OR writes > 0 OR extends > 0 OR hits > 0`, strings.Join(quoted, ", ")))
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]*string, len(columns))
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return Result{}, err
		}
		labels := map[string]string{"backend_type": stringPointer(values[0]), "object": stringPointer(values[1]), "context": stringPointer(values[2])}
		for i := 3; i < len(columns); i++ {
			if values[i] == nil {
				continue
			}
			parsed, err := strconv.ParseFloat(*values[i], 64)
			if err != nil {
				log.Printf("io: skipping non-numeric pg_stat_io column %q value %q", columns[i], *values[i])
				continue
			}
			result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_io_" + columns[i], Value: parsed, Kind: pgtype.KindCounter, Labels: labels})
		}
	}
	return result, rows.Err()
}

func probeIOColumns(ctx context.Context, conn Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = 'pg_catalog' AND table_name = 'pg_stat_io'
ORDER BY ordinal_position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	available := make(map[string]bool)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, err
		}
		available[column] = true
	}
	return available, rows.Err()
}

func stringPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
