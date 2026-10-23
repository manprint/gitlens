package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// internalsWindow is the span the internals rates are averaged over.
const internalsWindow = time.Hour

type walDirectoryAPIEntry struct {
	Bytes      float64   `json:"bytes"`
	Files      float64   `json:"files"`
	ObservedAt time.Time `json:"observed_at"`
}

type slruAPIEntry struct {
	Name string `json:"name"`
	// HitRatio is nil when the cache was not read in the window.
	HitRatio           *float64  `json:"hit_ratio"`
	HitsPerSecond      float64   `json:"hits_per_second"`
	ReadsPerSecond     float64   `json:"reads_per_second"`
	WritesPerSecond    float64   `json:"writes_per_second"`
	ZeroedPerSecond    float64   `json:"zeroed_per_second"`
	FlushesPerSecond   float64   `json:"flushes_per_second"`
	TruncatesPerSecond float64   `json:"truncates_per_second"`
	ExistsPerSecond    float64   `json:"exists_per_second"`
	LastSeen           time.Time `json:"last_seen"`
}

type databaseSizeAPIEntry struct {
	Database   string    `json:"database"`
	SizeBytes  float64   `json:"size_bytes"`
	ObservedAt time.Time `json:"observed_at"`
	// Growth24h and Growth7d are nil without a size from that long ago.
	Growth24h *float64 `json:"growth_24h_bytes"`
	Growth7d  *float64 `json:"growth_7d_bytes"`
}

type tempFilesAPIEntry struct {
	Database       string  `json:"database"`
	FilesPerSecond float64 `json:"files_per_second"`
	BytesPerSecond float64 `json:"bytes_per_second"`
}

type recoveryConflictAPIEntry struct {
	Database  string  `json:"database"`
	Reason    string  `json:"reason"`
	PerSecond float64 `json:"per_second"`
}

type standbyAPIEntry struct {
	ReplayPaused bool                       `json:"replay_paused"`
	ObservedAt   time.Time                  `json:"observed_at"`
	Conflicts    []recoveryConflictAPIEntry `json:"conflicts"`
	// PrefetchBlocksPerSecond maps each outcome of the WAL prefetcher
	// (prefetch, hit, skip_*) to its rate.
	PrefetchBlocksPerSecond map[string]float64 `json:"prefetch_blocks_per_second"`
}

type internalsAPIResponse struct {
	InstanceID    string                 `json:"instance_id"`
	WindowSeconds int                    `json:"window_seconds"`
	WALDirectory  *walDirectoryAPIEntry  `json:"wal_directory"`
	Databases     []databaseSizeAPIEntry `json:"databases"`
	SLRU          []slruAPIEntry         `json:"slru"`
	TempFiles     []tempFilesAPIEntry    `json:"temp_files"`
	// Standby is nil unless the instance replayed WAL in the window.
	Standby *standbyAPIEntry `json:"standby"`
}

func (a *API) handleInternals(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	response := internalsAPIResponse{InstanceID: id.String(), WindowSeconds: int(internalsWindow.Seconds()),
		Databases: []databaseSizeAPIEntry{}, SLRU: []slruAPIEntry{}, TempFiles: []tempFilesAPIEntry{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	ctx := r.Context()
	for _, load := range []func(context.Context, uuid.UUID, *internalsAPIResponse) error{
		a.loadWALDirectory, a.loadDatabaseSizes, a.loadSLRU, a.loadTempFiles, a.loadStandby,
	} {
		if err := load(ctx, id, &response); err != nil {
			writeError(w, http.StatusInternalServerError, "internals query failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) loadWALDirectory(ctx context.Context, id uuid.UUID, response *internalsAPIResponse) error {
	rows, err := a.pool.Query(ctx, `SELECT DISTINCT ON (metric) metric, value, ts
		FROM metrics
		WHERE tenant_id = 'default' AND instance_id = $1 AND metric IN ('pg_wal_dir_bytes', 'pg_wal_dir_files')
			AND ts >= now() - make_interval(secs => $2)
		ORDER BY metric, ts DESC`, id, internalsWindow.Seconds())
	if err != nil {
		return err
	}
	defer rows.Close()
	var entry walDirectoryAPIEntry
	found := false
	for rows.Next() {
		var metric string
		var value float64
		var ts time.Time
		if err := rows.Scan(&metric, &value, &ts); err != nil {
			return err
		}
		found = true
		if metric == "pg_wal_dir_bytes" {
			entry.Bytes, entry.ObservedAt = value, ts
		} else {
			entry.Files = value
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		response.WALDirectory = &entry
	}
	return nil
}

// databaseSizeFresh is how old a database's latest size may be to be listed:
// the check runs every fifteen minutes, and a database dropped since is left
// out.
const databaseSizeFresh = 2 * time.Hour

// loadDatabaseSizes lists each database's latest size and its growth since
// the size measured closest to 24 hours and 7 days before it, within two
// hours.
func (a *API) loadDatabaseSizes(ctx context.Context, id uuid.UUID, response *internalsAPIResponse) error {
	rows, err := a.pool.Query(ctx, `WITH latest AS (
			SELECT DISTINCT ON (labels->>'database') labels->>'database' AS db, value, ts
			FROM metrics
			WHERE tenant_id = 'default' AND instance_id = $1 AND metric = 'pg_database_size_bytes'
				AND ts >= now() - make_interval(secs => $2)
			ORDER BY labels->>'database', ts DESC
		)
		SELECT l.db, l.value, l.ts,
			l.value - (SELECT m.value FROM metrics m
				WHERE m.tenant_id = 'default' AND m.instance_id = $1 AND m.metric = 'pg_database_size_bytes'
					AND m.labels->>'database' = l.db
					AND m.ts <= l.ts - interval '24 hours' AND m.ts > l.ts - interval '26 hours'
				ORDER BY m.ts DESC LIMIT 1),
			l.value - (SELECT m.value FROM metrics m
				WHERE m.tenant_id = 'default' AND m.instance_id = $1 AND m.metric = 'pg_database_size_bytes'
					AND m.labels->>'database' = l.db
					AND m.ts <= l.ts - interval '7 days' AND m.ts > l.ts - interval '7 days 2 hours'
				ORDER BY m.ts DESC LIMIT 1)
		FROM latest l
		ORDER BY l.value DESC, l.db`, id, databaseSizeFresh.Seconds())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e databaseSizeAPIEntry
		if err := rows.Scan(&e.Database, &e.SizeBytes, &e.ObservedAt, &e.Growth24h, &e.Growth7d); err != nil {
			return err
		}
		response.Databases = append(response.Databases, e)
	}
	return rows.Err()
}

func (a *API) loadSLRU(ctx context.Context, id uuid.UUID, response *internalsAPIResponse) error {
	rows, err := a.pool.Query(ctx, `SELECT labels->>'slru',
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_slru_blks_hit_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_slru_blks_read_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_slru_blks_written_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_slru_blks_zeroed_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_slru_flushes_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_slru_truncates_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_slru_blks_exists_total'), 0),
			max(ts)
		FROM metrics
		WHERE tenant_id = 'default' AND instance_id = $1 AND labels ? 'slru'
			AND metric IN ('pg_slru_blks_hit_total', 'pg_slru_blks_read_total', 'pg_slru_blks_written_total', 'pg_slru_blks_zeroed_total',
				'pg_slru_flushes_total', 'pg_slru_truncates_total', 'pg_slru_blks_exists_total')
			AND ts >= now() - make_interval(secs => $2)
		GROUP BY 1
		ORDER BY 3 DESC, 1`, id, internalsWindow.Seconds())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e slruAPIEntry
		if err := rows.Scan(&e.Name, &e.HitsPerSecond, &e.ReadsPerSecond, &e.WritesPerSecond, &e.ZeroedPerSecond,
			&e.FlushesPerSecond, &e.TruncatesPerSecond, &e.ExistsPerSecond, &e.LastSeen); err != nil {
			return err
		}
		if total := e.HitsPerSecond + e.ReadsPerSecond; total > 0 {
			ratio := e.HitsPerSecond / total
			e.HitRatio = &ratio
		}
		response.SLRU = append(response.SLRU, e)
	}
	return rows.Err()
}

func (a *API) loadTempFiles(ctx context.Context, id uuid.UUID, response *internalsAPIResponse) error {
	rows, err := a.pool.Query(ctx, `SELECT labels->>'database',
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_temp_files_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_temp_bytes_total'), 0)
		FROM metrics
		WHERE tenant_id = 'default' AND instance_id = $1 AND labels ? 'database'
			AND metric IN ('pg_temp_files_total', 'pg_temp_bytes_total')
			AND ts >= now() - make_interval(secs => $2)
		GROUP BY 1
		ORDER BY 3 DESC, 2 DESC, 1`, id, internalsWindow.Seconds())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e tempFilesAPIEntry
		if err := rows.Scan(&e.Database, &e.FilesPerSecond, &e.BytesPerSecond); err != nil {
			return err
		}
		response.TempFiles = append(response.TempFiles, e)
	}
	return rows.Err()
}

func (a *API) loadStandby(ctx context.Context, id uuid.UUID, response *internalsAPIResponse) error {
	var entry standbyAPIEntry
	var paused float64
	err := a.pool.QueryRow(ctx, `SELECT value, ts FROM metrics
		WHERE tenant_id = 'default' AND instance_id = $1 AND metric = 'pg_recovery_replay_paused'
			AND ts >= now() - make_interval(secs => $2)
		ORDER BY ts DESC LIMIT 1`, id, internalsWindow.Seconds()).Scan(&paused, &entry.ObservedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	entry.ReplayPaused = paused >= 1
	entry.Conflicts = []recoveryConflictAPIEntry{}
	entry.PrefetchBlocksPerSecond = map[string]float64{}

	rows, err := a.pool.Query(ctx, `SELECT labels->>'database', labels->>'reason', avg(value)
		FROM metrics
		WHERE tenant_id = 'default' AND instance_id = $1 AND metric = 'pg_recovery_conflicts_total'
			AND ts >= now() - make_interval(secs => $2)
		GROUP BY 1, 2
		HAVING avg(value) > 0
		ORDER BY 3 DESC, 1, 2`, id, internalsWindow.Seconds())
	if err != nil {
		return err
	}
	for rows.Next() {
		var c recoveryConflictAPIEntry
		if err := rows.Scan(&c.Database, &c.Reason, &c.PerSecond); err != nil {
			rows.Close()
			return err
		}
		entry.Conflicts = append(entry.Conflicts, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = a.pool.Query(ctx, `SELECT labels->>'outcome', avg(value)
		FROM metrics
		WHERE tenant_id = 'default' AND instance_id = $1 AND metric = 'pg_recovery_prefetch_blocks_total'
			AND ts >= now() - make_interval(secs => $2)
		GROUP BY 1`, id, internalsWindow.Seconds())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var outcome string
		var rate float64
		if err := rows.Scan(&outcome, &rate); err != nil {
			return err
		}
		entry.PrefetchBlocksPerSecond[outcome] = rate
	}
	if err := rows.Err(); err != nil {
		return err
	}
	response.Standby = &entry
	return nil
}
