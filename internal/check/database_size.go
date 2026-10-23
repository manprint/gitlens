package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&databaseSizeCheck{}) }

// databaseSizeQuery sizes each database that accepts connections.
// pg_database_size adds up the database's files without reading them and
// takes no lock. It needs CONNECT on the database or pg_read_all_stats (in
// pg_monitor); a database the role may not size is left out rather than
// failing the query for every other.
const databaseSizeQuery = `SELECT d.datname, pg_catalog.pg_database_size(d.oid)
  FROM pg_catalog.pg_database d
 WHERE d.datallowconn AND NOT d.datistemplate
   AND (pg_catalog.has_database_privilege(d.oid, 'CONNECT')
        OR pg_catalog.pg_has_role('pg_read_all_stats', 'MEMBER'))`

// databaseSizeCheck reports how much disk each database takes. The size
// walks every file of the database, so it runs every fifteen minutes; the
// server derives growth from the stored history.
type databaseSizeCheck struct{}

func (c *databaseSizeCheck) Name() string { return "database_size" }
func (c *databaseSizeCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *databaseSizeCheck) DefaultInterval() time.Duration { return 15 * time.Minute }
func (c *databaseSizeCheck) Timeout() time.Duration         { return 30 * time.Second }

func (c *databaseSizeCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, databaseSizeQuery)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var result Result
	for rows.Next() {
		var datname string
		var size int64
		if err := rows.Scan(&datname, &size); err != nil {
			return Result{}, err
		}
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_database_size_bytes", Value: float64(size), Kind: pgtype.KindGauge,
			Labels: map[string]string{"database": datname}})
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}
