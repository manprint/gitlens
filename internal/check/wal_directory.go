package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&walDirectoryCheck{}) }

// walDirectoryQuery sizes pg_wal: the segments, partial segments and
// history files it holds. pg_ls_waldir needs pg_monitor (or an explicit
// grant) and reads only the directory, never a file.
const walDirectoryQuery = `SELECT pg_catalog.count(*)::float8, COALESCE(pg_catalog.sum(size), 0)::float8
  FROM pg_catalog.pg_ls_waldir()`

// walDirectoryCheck reports how much WAL the server keeps. It grows past
// max_wal_size when something holds WAL back: a replication slot whose
// consumer is behind or gone, an archive_command that fails, or
// wal_keep_size. The listing runs once a minute, apart from the 15-second
// wal check, because its cost grows with the number of segments.
type walDirectoryCheck struct{}

func (c *walDirectoryCheck) Name() string { return "wal_directory" }
func (c *walDirectoryCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *walDirectoryCheck) DefaultInterval() time.Duration { return time.Minute }
func (c *walDirectoryCheck) Timeout() time.Duration         { return 10 * time.Second }

func (c *walDirectoryCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	var files, bytes float64
	if err := conn.QueryRow(ctx, walDirectoryQuery).Scan(&files, &bytes); err != nil {
		// Without EXECUTE on pg_ls_waldir (a managed service may withhold it
		// from pg_monitor) the metrics are left out rather than failing the
		// scrape every minute, as the archiver's ready-file count is.
		if code := pgErrorCode(err); code == pgInsufficientPrivilege || code == pgUndefinedFunction {
			return Result{}, nil
		}
		return Result{}, err
	}
	return Result{Metrics: []pgtype.Metric{
		{Name: "pg_wal_dir_bytes", Value: bytes, Kind: pgtype.KindGauge},
		{Name: "pg_wal_dir_files", Value: files, Kind: pgtype.KindGauge},
	}}, nil
}
