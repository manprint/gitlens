package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&checkpointerCheck{}) }

type checkpointerCheck struct{}

func (c *checkpointerCheck) Name() string { return "checkpointer" }
func (c *checkpointerCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *checkpointerCheck) DefaultInterval() time.Duration { return 30 * time.Second }
func (c *checkpointerCheck) Timeout() time.Duration         { return 5 * time.Second }

type checkpointCounters struct {
	timed, requested, writeTime, syncTime, buffersClean, maxWrittenClean, buffersAlloc float64
}

func (c *checkpointerCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	var counters checkpointCounters
	if t.PGVersion() >= pgtype.PG17 {
		row := conn.QueryRow(ctx, `SELECT num_timed, num_requested, write_time, sync_time FROM pg_stat_checkpointer`)
		if err := row.Scan(&counters.timed, &counters.requested, &counters.writeTime, &counters.syncTime); err != nil {
			return Result{}, err
		}
	} else {
		row := conn.QueryRow(ctx, `SELECT checkpoints_timed, checkpoints_req, checkpoint_write_time, checkpoint_sync_time FROM pg_stat_bgwriter`)
		if err := row.Scan(&counters.timed, &counters.requested, &counters.writeTime, &counters.syncTime); err != nil {
			return Result{}, err
		}
	}
	row := conn.QueryRow(ctx, `SELECT buffers_clean, maxwritten_clean, buffers_alloc FROM pg_stat_bgwriter`)
	if err := row.Scan(&counters.buffersClean, &counters.maxWrittenClean, &counters.buffersAlloc); err != nil {
		return Result{}, err
	}
	return Result{Metrics: []pgtype.Metric{
		{Name: "pg_checkpoints_timed_total", Value: counters.timed, Kind: pgtype.KindCounter},
		{Name: "pg_checkpoints_requested_total", Value: counters.requested, Kind: pgtype.KindCounter},
		{Name: "pg_checkpoint_write_time_ms_total", Value: counters.writeTime, Kind: pgtype.KindCounter},
		{Name: "pg_checkpoint_sync_time_ms_total", Value: counters.syncTime, Kind: pgtype.KindCounter},
		{Name: "pg_bgwriter_buffers_clean_total", Value: counters.buffersClean, Kind: pgtype.KindCounter},
		{Name: "pg_bgwriter_maxwritten_clean_total", Value: counters.maxWrittenClean, Kind: pgtype.KindCounter},
		{Name: "pg_buffers_alloc_total", Value: counters.buffersAlloc, Kind: pgtype.KindCounter},
	}}, nil
}
