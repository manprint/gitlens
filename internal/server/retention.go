package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Data retention (11.3, D17): how many days of history each metrics family
// keeps. A family is one hypertable; its TimescaleDB retention policy drops a
// whole chunk once the chunk's end is older than drop_after, at the policy's
// next daily run, never at the write. A change of days alters the existing job
// in place, so it keeps its schedule and next start; a policy given to a family
// that has none first runs one day after it is created. Only the families
// below can be read or changed, and a family reaches SQL only as a bound
// parameter (I-3).

// retentionFamily is one metrics family: the hypertable it maps to and the
// retention its migration gives it.
type retentionFamily struct {
	Name        string
	Hypertable  string
	DefaultDays int
}

// retentionFamilies is the whitelist, in the order the API lists it (D17). The
// defaults are those of migrations 0003 and 0007.
var retentionFamilies = []retentionFamily{
	{Name: "metrics", Hypertable: "metrics", DefaultDays: 30},
	{Name: "metrics_statements", Hypertable: "metrics_statements", DefaultDays: 30},
	{Name: "metrics_ash", Hypertable: "metrics_ash", DefaultDays: 30},
	{Name: "metrics_replication", Hypertable: "metrics_replication", DefaultDays: 30},
	{Name: "metrics_tables", Hypertable: "metrics_tables", DefaultDays: 30},
	{Name: "metrics_indexes", Hypertable: "metrics_indexes", DefaultDays: 30},
	{Name: "metrics_bloat", Hypertable: "metrics_bloat", DefaultDays: 90},
}

const (
	// retentionMaxDays is the longest retention the console sets.
	retentionMaxDays = 365
	// retentionFloorDays is the shortest retention of a family, whatever its
	// compression policy.
	retentionFloorDays = 3
	secondsPerDay      = 86400
	// retentionAttempts bounds the replays of a write that found the policy
	// changed under it by another write.
	retentionAttempts = 3
)

// lookupRetentionFamily returns the whitelisted family of that name.
func lookupRetentionFamily(name string) (retentionFamily, bool) {
	for _, f := range retentionFamilies {
		if f.Name == name {
			return f, true
		}
	}
	return retentionFamily{}, false
}

// retentionHypertables lists the whitelisted hypertables, the parameter of the
// policy read.
func retentionHypertables() []string {
	out := make([]string, 0, len(retentionFamilies))
	for _, f := range retentionFamilies {
		out = append(out, f.Hypertable)
	}
	return out
}

// livePolicy is what TimescaleDB holds for one hypertable, in seconds. A nil
// field means the hypertable has no such policy.
type livePolicy struct {
	DropAfter     *float64
	CompressAfter *float64
	// Unsupported describes a retention policy without drop_after (one set
	// outside the console with drop_created_before): the hypertable has a
	// policy, but not one the console can read in days or replace. Empty when
	// there is none.
	Unsupported string
}

// chunkPreview is what a retention of some days would drop from a hypertable
// now: show_chunks with older_than, sized by chunks_detailed_size.
type chunkPreview struct {
	Chunks int64
	Bytes  int64
	// OldestChunkAt is the start of the hypertable's oldest chunk, dropped or
	// not; nil when the hypertable has no chunk.
	OldestChunkAt *time.Time
	// NewestDroppedEnd is the end of the newest chunk that would be dropped.
	NewestDroppedEnd *time.Time
}

// errRetentionChanged is returned by Replace when the hypertable's policy is no
// longer the one the caller decided on.
var errRetentionChanged = errors.New("the retention policy changed while it was being set")

// errRetentionUnsupported is returned by Replace when the hypertable has a
// retention policy without drop_after, which the console never replaces.
var errRetentionUnsupported = errors.New("the hypertable has a retention policy without drop_after")

// policyStore is the TimescaleDB side of retention. The handlers decide what
// to do; the store reads, changes and creates policies. Unit tests stub it.
type policyStore interface {
	// Policies reads the retention and compression policy of each whitelisted
	// hypertable, keyed by hypertable name.
	Policies(ctx context.Context) (map[string]livePolicy, error)
	// Preview counts the chunks a retention of days would drop now.
	Preview(ctx context.Context, hypertable string, days int) (chunkPreview, error)
	// Replace sets the hypertable's retention to days in one transaction:
	// an existing policy is altered in place (same job, schedule and next
	// start), a missing one is created with its first run a day away. seen is
	// the drop_after the caller decided on (nil: no policy); a different live
	// value aborts with errRetentionChanged. Any failure rolls back and leaves
	// the old policy in place.
	Replace(ctx context.Context, hypertable string, days int, seen *float64) error
}

// The SQL of the policy store. Every value is a bound parameter: the
// hypertable name (always from retentionFamilies) and the number of days. The
// static test RET-005 guards that no statement is built from strings.
const (
	// retentionPoliciesSQL reads drop_after and the compression horizon in
	// seconds (compress_after, or compress_created_before for a policy set on
	// creation time), and describes a retention policy that has no drop_after.
	retentionPoliciesSQL = `SELECT j.hypertable_name, j.proc_name,
		extract(epoch FROM (CASE j.proc_name
			WHEN 'policy_retention' THEN j.config->>'drop_after'
			ELSE coalesce(j.config->>'compress_after', j.config->>'compress_created_before') END)::interval)::float8,
		CASE WHEN j.proc_name = 'policy_retention' AND j.config->>'drop_after' IS NULL
			THEN coalesce('drop_created_before ' || (j.config->>'drop_created_before'), j.config::text) END
	  FROM timescaledb_information.jobs j
	 WHERE j.proc_name IN ('policy_retention', 'policy_compression')
	   AND j.hypertable_schema = 'public'
	   AND j.hypertable_name = ANY($1::text[])`

	// retentionPreviewSQL counts and sizes the chunks show_chunks would return
	// for older_than = days, and reads the oldest chunk of the hypertable.
	retentionPreviewSQL = `WITH h AS (
		SELECT format('public.%I', $1::text)::regclass AS rel
	), dropped AS (
		SELECT s.chunk FROM h, show_chunks(h.rel, older_than => $2::int * interval '1 day') AS s(chunk)
	), sized AS (
		SELECT format('%I.%I', d.chunk_schema, d.chunk_name)::regclass AS chunk, d.total_bytes
		  FROM h, chunks_detailed_size(h.rel) AS d
	), ranged AS (
		SELECT format('%I.%I', c.chunk_schema, c.chunk_name)::regclass AS chunk, c.range_start, c.range_end
		  FROM timescaledb_information.chunks c
		 WHERE c.hypertable_schema = 'public' AND c.hypertable_name = $1::text
	)
	SELECT (SELECT count(*) FROM dropped),
	       (SELECT coalesce(sum(sized.total_bytes), 0)::bigint FROM sized JOIN dropped USING (chunk)),
	       (SELECT min(range_start) FROM ranged),
	       (SELECT max(ranged.range_end) FROM ranged JOIN dropped USING (chunk))`

	// retentionLockSQL serialises retention writes, so two writes never both
	// change, or both create, the policy of the same hypertable.
	retentionLockSQL = `SELECT pg_advisory_xact_lock(hashtext('pglens.retention'))`

	// retentionCurrentSQL reads the live drop_after of one hypertable inside
	// the write's transaction (NULL when it has no retention policy) and the
	// number of its retention policies without drop_after.
	retentionCurrentSQL = `SELECT min(extract(epoch FROM (j.config->>'drop_after')::interval))::float8,
	       count(*) FILTER (WHERE j.config->>'drop_after' IS NULL)
	  FROM timescaledb_information.jobs j
	 WHERE j.proc_name = 'policy_retention'
	   AND j.hypertable_schema = 'public'
	   AND j.hypertable_name = $1::text`

	// retentionAlterSQL sets drop_after of the hypertable's retention job in
	// place with alter_job, which changes only the config: the job keeps its
	// id, schedule and next_start, so no run starts at the write (a job that
	// add_retention_policy creates without initial_start runs at once). The
	// value is the interval as text, the form add_retention_policy stores
	// ("14 days") and the policy parses at each run. It answers the number of
	// jobs altered: with if_exists, a job removed meanwhile answers no row, so
	// it counts 0 and the write is decided again instead of failing.
	retentionAlterSQL = `SELECT count(a.job_id)
	  FROM timescaledb_information.jobs j
	 CROSS JOIN LATERAL alter_job(j.job_id,
	       config => jsonb_set(j.config, '{drop_after}', to_jsonb(make_interval(days => $2::int)::text)),
	       if_exists => true) AS a
	 WHERE j.proc_name = 'policy_retention'
	   AND j.hypertable_schema = 'public'
	   AND j.hypertable_name = $1::text
	   AND j.config->>'drop_after' IS NOT NULL`

	// retentionAddSQL creates the retention policy of a hypertable that has
	// none. Its first run is one day away, the policy's own interval, so a new
	// policy deletes nothing at the write either. The policy is added without
	// initial_start, so it is not fixed_schedule and runs a day after each run
	// ends, as the jobs of the migrations do; alter_job sets its first
	// next_start in the same transaction, before the scheduler can see the
	// job. (initial_start would make it fixed_schedule, and turning that off
	// with alter_job starts a run at once.) It answers the job id.
	retentionAddSQL = `SELECT a.job_id
	  FROM (SELECT add_retention_policy(format('public.%I', $1::text)::regclass, drop_after => make_interval(days => $2::int)) AS job_id) n
	 CROSS JOIN LATERAL alter_job(n.job_id, next_start => now() + interval '1 day') AS a`
)

// pgPolicyStore is the policy store on the pglens database.
type pgPolicyStore struct {
	pool dbPool
}

func (s pgPolicyStore) Policies(ctx context.Context) (map[string]livePolicy, error) {
	rows, err := s.pool.Query(ctx, retentionPoliciesSQL, retentionHypertables())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]livePolicy, len(retentionFamilies))
	for rows.Next() {
		var (
			hypertable, proc string
			seconds          *float64
			unsupported      *string
		)
		if err := rows.Scan(&hypertable, &proc, &seconds, &unsupported); err != nil {
			return nil, err
		}
		if unsupported != nil {
			p := out[hypertable]
			p.Unsupported = *unsupported
			out[hypertable] = p
			continue
		}
		if seconds == nil {
			continue
		}
		p := out[hypertable]
		switch proc {
		case "policy_retention":
			// Only one retention policy can exist; the shortest wins should
			// there ever be more, as it is the one that deletes.
			if p.DropAfter == nil || *seconds < *p.DropAfter {
				p.DropAfter = seconds
			}
		case "policy_compression":
			// The latest compression horizon bounds the retention.
			if p.CompressAfter == nil || *seconds > *p.CompressAfter {
				p.CompressAfter = seconds
			}
		}
		out[hypertable] = p
	}
	return out, rows.Err()
}

func (s pgPolicyStore) Preview(ctx context.Context, hypertable string, days int) (chunkPreview, error) {
	var p chunkPreview
	err := s.pool.QueryRow(ctx, retentionPreviewSQL, hypertable, days).
		Scan(&p.Chunks, &p.Bytes, &p.OldestChunkAt, &p.NewestDroppedEnd)
	return p, err
}

func (s pgPolicyStore) Replace(ctx context.Context, hypertable string, days int, seen *float64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	// A no-op after Commit; on any error the old policy stays as it was.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, retentionLockSQL); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	var (
		current     *float64
		unsupported int64
	)
	if err := tx.QueryRow(ctx, retentionCurrentSQL, hypertable).Scan(&current, &unsupported); err != nil {
		return fmt.Errorf("read policy: %w", err)
	}
	if unsupported > 0 {
		return errRetentionUnsupported
	}
	if !sameSeconds(current, seen) {
		return errRetentionChanged
	}
	if current != nil {
		var altered int64
		if err := tx.QueryRow(ctx, retentionAlterSQL, hypertable, days).Scan(&altered); err != nil {
			return fmt.Errorf("alter_job: %w", err)
		}
		if altered == 0 {
			// The policy read under the lock is gone: nothing was changed.
			return errRetentionChanged
		}
		return tx.Commit(ctx)
	}
	var jobID int64
	if err := tx.QueryRow(ctx, retentionAddSQL, hypertable, days).Scan(&jobID); err != nil {
		return fmt.Errorf("add_retention_policy: %w", err)
	}
	return tx.Commit(ctx)
}

func sameSeconds(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// retentionStore is the policy store of the API: the stub of a test, else the
// database, else nil (no store).
func (a *API) retentionStore() policyStore {
	if a.retention != nil {
		return a.retention
	}
	if a.pool == nil {
		return nil
	}
	return pgPolicyStore{pool: a.pool}
}

func (a *API) registerRetentionRoutes(r chi.Router) {
	r.Get("/api/v1/settings/retention", a.handleGetRetention)
	r.Get("/api/v1/settings/retention/preview", a.handlePreviewRetention)
	r.Put("/api/v1/settings/retention/{family}", a.handleSetRetention)
}

// retentionRow is one family as getRetention lists it.
type retentionRow struct {
	Family     string `json:"family"`
	Hypertable string `json:"hypertable"`
	// Days is null when the hypertable has no retention policy, or one
	// without drop_after (UnsupportedPolicy then describes it).
	Days    *int   `json:"days"`
	Source  string `json:"source"`
	MinDays int    `json:"min_days"`
	MaxDays int    `json:"max_days"`
	// UnsupportedPolicy is set when the hypertable's retention policy has no
	// drop_after: the console shows it and does not replace it.
	UnsupportedPolicy string `json:"unsupported_policy,omitempty"`
}

// retentionPreviewResp is previewRetention's answer.
type retentionPreviewResp struct {
	Family                string     `json:"family"`
	Days                  int        `json:"days"`
	ChunksToDrop          int64      `json:"chunks_to_drop"`
	BytesToFree           int64      `json:"bytes_to_free"`
	OldestChunkAt         *time.Time `json:"oldest_chunk_at"`
	NewestDroppedChunkEnd *time.Time `json:"newest_dropped_chunk_end"`
}

// retentionConfirmationRequired is the 409 of a shortening sent without
// confirm: true (RetentionConfirmationRequired).
type retentionConfirmationRequired struct {
	Error   string               `json:"error"`
	Detail  string               `json:"detail"`
	Preview retentionPreviewResp `json:"preview"`
}

// minRetentionDays is the shortest retention a family takes: one day past its
// compression horizon, and never under three days. Without a compression
// policy the floor applies.
func minRetentionDays(compressAfter *float64) int {
	if compressAfter == nil {
		return retentionFloorDays
	}
	return max(retentionFloorDays, int(math.Ceil(*compressAfter/secondsPerDay))+1)
}

// retentionDays is a drop_after in whole days, rounded up so a policy set
// outside the console to a part of a day still reads as no shorter than it is.
func retentionDays(dropAfter *float64) *int {
	if dropAfter == nil {
		return nil
	}
	d := int(math.Ceil(*dropAfter / secondsPerDay))
	return &d
}

func renderRetentionRow(f retentionFamily, p livePolicy) retentionRow {
	row := retentionRow{
		Family:     f.Name,
		Hypertable: f.Hypertable,
		Days:       retentionDays(p.DropAfter),
		Source:     "custom",
		MinDays:    minRetentionDays(p.CompressAfter),
		MaxDays:    retentionMaxDays,
		// A policy the console cannot read in days is custom by definition.
		UnsupportedPolicy: p.Unsupported,
	}
	if p.Unsupported == "" && p.DropAfter != nil && *p.DropAfter == float64(f.DefaultDays*secondsPerDay) {
		row.Source = "default"
	}
	return row
}

// isShortening reports whether days keeps less than the live policy. A family
// without a policy keeps everything, so any retention shortens it.
func isShortening(dropAfter *float64, days int) bool {
	return dropAfter == nil || float64(days*secondsPerDay) < *dropAfter
}

// writeRetentionStoreError answers a failed policy read.
func writeRetentionStoreError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusServiceUnavailable, "store unavailable", err.Error())
}

// writeRetentionUnsupported answers a preview or a write of a family whose
// retention policy has no drop_after: the console leaves it as it is.
func writeRetentionUnsupported(w http.ResponseWriter, f retentionFamily, policy string) {
	writeError(w, http.StatusServiceUnavailable, "unsupported retention policy",
		fmt.Sprintf("%s has a retention policy pglens does not replace (%s); change or remove it with TimescaleDB first", f.Name, policy))
}

// checkStaticRetentionDays answers 400 when days is outside the range every
// family shares, before the store is read: such a value is refused even with
// the store down.
func checkStaticRetentionDays(w http.ResponseWriter, f retentionFamily, days int) bool {
	return checkRetentionDays(w, f, days, retentionFloorDays)
}

// checkRetentionDays answers 400 when days is outside the family's range.
func checkRetentionDays(w http.ResponseWriter, f retentionFamily, days, minDays int) bool {
	if days < minDays || days > retentionMaxDays {
		writeError(w, http.StatusBadRequest, "invalid days",
			fmt.Sprintf("days of %s must be between %d and %d", f.Name, minDays, retentionMaxDays))
		return false
	}
	return true
}

// handleGetRetention lists the seven families with their live retention.
func (a *API) handleGetRetention(w http.ResponseWriter, r *http.Request) {
	ps := a.retentionStore()
	if ps == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	policies, err := ps.Policies(ctx)
	if err != nil {
		writeRetentionStoreError(w, err)
		return
	}
	out := make([]retentionRow, 0, len(retentionFamilies))
	for _, f := range retentionFamilies {
		out = append(out, renderRetentionRow(f, policies[f.Hypertable]))
	}
	writeJSON(w, http.StatusOK, out)
}

// previewOf answers what a retention of days drops now. A retention that is
// not shorter than the live one drops nothing more than the live one does, so
// its counts are zero.
func previewOf(ctx context.Context, ps policyStore, f retentionFamily, p livePolicy, days int) (retentionPreviewResp, error) {
	c, err := ps.Preview(ctx, f.Hypertable, days)
	if err != nil {
		return retentionPreviewResp{}, err
	}
	out := retentionPreviewResp{Family: f.Name, Days: days, OldestChunkAt: c.OldestChunkAt}
	if isShortening(p.DropAfter, days) {
		out.ChunksToDrop = c.Chunks
		out.BytesToFree = c.Bytes
		out.NewestDroppedChunkEnd = c.NewestDroppedEnd
	}
	return out, nil
}

// handlePreviewRetention answers what a retention of days would drop.
func (a *API) handlePreviewRetention(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("family")
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid family", "family is required")
		return
	}
	f, ok := lookupRetentionFamily(name)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown family", name)
		return
	}
	days, err := strconv.Atoi(q.Get("days"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid days", "days must be an integer number of days")
		return
	}
	if !checkStaticRetentionDays(w, f, days) {
		return
	}
	ps := a.retentionStore()
	if ps == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	policies, err := ps.Policies(ctx)
	if err != nil {
		writeRetentionStoreError(w, err)
		return
	}
	p := policies[f.Hypertable]
	if p.Unsupported != "" {
		writeRetentionUnsupported(w, f, p.Unsupported)
		return
	}
	if !checkRetentionDays(w, f, days, minRetentionDays(p.CompressAfter)) {
		return
	}
	out, err := previewOf(ctx, ps, f, p, days)
	if err != nil {
		writeRetentionStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type retentionBody struct {
	Days    *int `json:"days"`
	Confirm bool `json:"confirm"`
}

// handleSetRetention sets a family's retention policy. Lengthening needs
// nothing more; shortening, or giving a retention to a family that has none,
// deletes history at the policy's next daily run (an existing policy keeps its
// schedule; a new one first runs a day later), so it needs confirm: true and is
// otherwise answered 409 with the preview. The same days answer the row
// without touching the policy.
func (a *API) handleSetRetention(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "family")
	f, ok := lookupRetentionFamily(name)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown family", name)
		return
	}
	var in retentionBody
	if !decodeStrictJSONBody(w, r, &in) {
		return
	}
	if in.Days == nil {
		writeError(w, http.StatusBadRequest, "invalid days", "days is required")
		return
	}
	days := *in.Days
	if !checkStaticRetentionDays(w, f, days) {
		return
	}
	ps := a.retentionStore()
	if ps == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	for range retentionAttempts {
		policies, err := ps.Policies(ctx)
		if err != nil {
			writeRetentionStoreError(w, err)
			return
		}
		p := policies[f.Hypertable]
		if p.Unsupported != "" {
			writeRetentionUnsupported(w, f, p.Unsupported)
			return
		}
		if !checkRetentionDays(w, f, days, minRetentionDays(p.CompressAfter)) {
			return
		}
		if p.DropAfter != nil && *p.DropAfter == float64(days*secondsPerDay) {
			writeJSON(w, http.StatusOK, renderRetentionRow(f, p))
			return
		}
		if isShortening(p.DropAfter, days) && !in.Confirm {
			preview, err := previewOf(ctx, ps, f, p, days)
			if err != nil {
				writeRetentionStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusConflict, retentionConfirmationRequired{
				Error:   "confirmation required",
				Detail:  fmt.Sprintf("a retention of %d days for %s deletes history at the next policy run; send confirm: true to apply it", days, f.Name),
				Preview: preview,
			})
			return
		}
		err = ps.Replace(ctx, f.Hypertable, days, p.DropAfter)
		if errors.Is(err, errRetentionChanged) {
			// Another write changed the policy since it was read: decide again.
			continue
		}
		if errors.Is(err, errRetentionUnsupported) {
			writeRetentionUnsupported(w, f, "a policy without drop_after")
			return
		}
		var pgErr *pgconn.PgError
		if err != nil && !errors.As(err, &pgErr) {
			// The store did not answer (connection, timeout): nothing was
			// changed, and the old policy stays.
			writeRetentionStoreError(w, err)
			return
		}
		if err != nil {
			// A policy call failed in the database and was rolled back.
			writeError(w, http.StatusInternalServerError, "retention update failed", err.Error())
			return
		}
		log.Printf("retention: %s set to %d days (was %s) by %s", f.Name, days, describeDropAfter(p.DropAfter), requesterOf(r, a.auth))
		seconds := float64(days * secondsPerDay)
		writeJSON(w, http.StatusOK, renderRetentionRow(f, livePolicy{DropAfter: &seconds, CompressAfter: p.CompressAfter}))
		return
	}
	writeError(w, http.StatusInternalServerError, "retention update failed", errRetentionChanged.Error()+"; retry")
}

func describeDropAfter(dropAfter *float64) string {
	if dropAfter == nil {
		return "no policy"
	}
	return strconv.FormatFloat(*dropAfter/secondsPerDay, 'f', -1, 64) + " days"
}
