package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/readiness"
	"github.com/manprint/pglens/internal/wire"
)

// readinessTTL is how long a computed report is served from memory (14.1).
const readinessTTL = 30 * time.Second

// readinessWindow bounds the facts and series a report reads.
const readinessWindow = "24 hours"

// readinessCache keeps the last report of each instance for readinessTTL. The
// zero value is ready to use; ttl and now are overridden by tests.
type readinessCache struct {
	mu      sync.Mutex
	entries map[uuid.UUID]readinessEntry
	ttl     time.Duration
	now     func() time.Time
}

type readinessEntry struct {
	report readiness.Report
	at     time.Time
}

func (c *readinessCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *readinessCache) lifetime() time.Duration {
	if c.ttl != 0 {
		return c.ttl
	}
	return readinessTTL
}

func (c *readinessCache) get(id uuid.UUID) (readiness.Report, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok || c.clock().Sub(e.at) >= c.lifetime() {
		return readiness.Report{}, false
	}
	return e.report, true
}

func (c *readinessCache) put(id uuid.UUID, r readiness.Report) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[uuid.UUID]readinessEntry{}
	}
	c.entries[id] = readinessEntry{report: r, at: c.clock()}
}

// errReadinessNotFound marks an instance the server does not know.
var errReadinessNotFound = errors.New("instance not found")

func (a *API) registerReadinessRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/readiness", a.handleReadiness)
	r.Get("/api/v1/readiness/summary", a.handleReadinessSummary)
}

func (a *API) handleReadiness(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	if a.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable", "the database pool is not available")
		return
	}
	report, err := a.readinessReport(r.Context(), id)
	switch {
	case errors.Is(err, errReadinessNotFound):
		writeError(w, http.StatusNotFound, "instance not found", id.String())
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "storage unavailable", err.Error())
	default:
		writeJSON(w, http.StatusOK, report)
	}
}

type readinessSummaryInstance struct {
	InstanceID string `json:"instance_id"`
	Level      string `json:"level"`
	Warnings   int    `json:"warnings"`
	Infos      int    `json:"infos"`
}

type readinessSummaryTotals struct {
	Full    int `json:"full"`
	Partial int `json:"partial"`
}

type readinessSummaryResponse struct {
	Instances []readinessSummaryInstance `json:"instances"`
	Totals    readinessSummaryTotals     `json:"totals"`
}

func (a *API) handleReadinessSummary(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable", "the database pool is not available")
		return
	}
	ctx := r.Context()
	rows, err := a.pool.Query(ctx, `SELECT instance_id FROM instances WHERE tenant_id='default' ORDER BY instance_id`)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable", err.Error())
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			writeError(w, http.StatusServiceUnavailable, "storage unavailable", scanErr.Error())
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable", err.Error())
		return
	}
	resp := readinessSummaryResponse{Instances: []readinessSummaryInstance{}}
	for _, id := range ids {
		report, err := a.readinessReport(ctx, id)
		if errors.Is(err, errReadinessNotFound) {
			continue // removed while the summary ran
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "storage unavailable", err.Error())
			return
		}
		resp.Instances = append(resp.Instances, readinessSummaryInstance{
			InstanceID: id.String(), Level: report.Level,
			Warnings: report.Counts.Warning, Infos: report.Counts.Info,
		})
		if report.Level == readiness.LevelFull {
			resp.Totals.Full++
		} else {
			resp.Totals.Partial++
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// readinessReport is the report of one instance, from the cache when it is
// younger than the TTL.
func (a *API) readinessReport(ctx context.Context, id uuid.UUID) (readiness.Report, error) {
	if report, ok := a.readinessCache.get(id); ok {
		return report, nil
	}
	in, err := a.readinessInput(ctx, id)
	if err != nil {
		return readiness.Report{}, err
	}
	in.Now = a.readinessCache.clock().UTC()
	report := readiness.Evaluate(in)
	a.readinessCache.put(id, report)
	return report, nil
}

// readinessInput assembles the evaluator's input from the latest facts and
// series of the last 24 hours. Every query is parameterised (I-3).
func (a *API) readinessInput(ctx context.Context, id uuid.UUID) (readiness.Input, error) {
	var (
		pgVersion int
		role      string
		permTier  string
	)
	err := a.pool.QueryRow(ctx,
		`SELECT pg_version, role, perm_tier FROM instances WHERE tenant_id='default' AND instance_id=$1`, id).
		Scan(&pgVersion, &role, &permTier)
	if errors.Is(err, pgx.ErrNoRows) {
		return readiness.Input{}, errReadinessNotFound
	}
	if err != nil {
		return readiness.Input{}, err
	}
	in := readiness.Input{
		Instance: readiness.Instance{
			ID: id.String(), PGMajor: pgtype.PGVersion(pgVersion).Major(),
			IsStandby: role == "standby", PermTier: -1,
		},
		Settings:       map[string]string{},
		SettingSources: map[string]string{},
		Extensions:     map[string][]string{},
	}
	if tier, parseErr := pgtype.ParsePermTier(permTier); parseErr == nil {
		in.Instance.PermTier = int(tier)
	}
	steps := []func(context.Context, uuid.UUID, *readiness.Input) error{
		a.readinessSettings, a.readinessExtensions, a.readinessRole, a.readinessSkips,
		a.readinessFailing, a.readinessHost, a.readinessFlags, a.readinessDatabases,
	}
	for _, step := range steps {
		if err := step(ctx, id, &in); err != nil {
			return readiness.Input{}, err
		}
	}
	channels := 0
	n := a.notificationState()
	for _, k := range notificationKinds {
		if n.row(k).Configured {
			channels++
		}
	}
	in.NotificationChannels = channels
	return in, nil
}

// readinessSettings reads the latest settings snapshot. The agent ships a
// setting only when it is not at its boot value (or is on its curated list),
// so a snapshot that exists says every other setting is at its default.
func (a *API) readinessSettings(ctx context.Context, id uuid.UUID, in *readiness.Input) error {
	rows, err := a.pool.Query(ctx, `SELECT key, COALESCE(value_text, ''), COALESCE(labels->>'source', '')
		FROM object_facts WHERE tenant_id='default' AND instance_id=$1 AND kind='setting'
		AND last_seen >= now() - interval '`+readinessWindow+`'
		AND last_seen >= (SELECT max(last_seen) FROM object_facts WHERE tenant_id='default' AND instance_id=$1 AND kind='setting') - interval '1 minute'`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, value, source string
		if err := rows.Scan(&name, &value, &source); err != nil {
			return err
		}
		in.Settings[name] = value
		if source != "" {
			in.SettingSources[name] = source
		}
		in.SettingsKnown = true
	}
	return rows.Err()
}

func (a *API) readinessExtensions(ctx context.Context, id uuid.UUID, in *readiness.Input) error {
	rows, err := a.pool.Query(ctx, `SELECT datname, key FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='extension'
		AND last_seen >= now() - interval '`+readinessWindow+`' ORDER BY datname, key`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var db, name string
		if err := rows.Scan(&db, &name); err != nil {
			return err
		}
		in.Extensions[db] = append(in.Extensions[db], name)
	}
	in.ExtensionsKnown = len(in.Extensions) > 0
	return rows.Err()
}

func boolLabel(s *string) *bool {
	if s == nil {
		return nil
	}
	switch *s {
	case "true":
		v := true
		return &v
	case "false":
		v := false
		return &v
	}
	return nil
}

func (a *API) readinessRole(ctx context.Context, id uuid.UUID, in *readiness.Input) error {
	var name, monitor, stats *string
	err := a.pool.QueryRow(ctx, `SELECT labels->>'role', labels->>'has_pg_monitor', labels->>'has_read_all_stats'
		FROM object_facts WHERE tenant_id='default' AND instance_id=$1 AND kind='monitoring_role'
		AND last_seen >= now() - interval '`+readinessWindow+`' ORDER BY last_seen DESC LIMIT 1`, id).
		Scan(&name, &monitor, &stats)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if name != nil {
		in.Instance.MonitoringRole = *name
	}
	in.Role = readiness.Role{HasPgMonitor: boolLabel(monitor), HasReadAllStats: boolLabel(stats)}
	return nil
}

func (a *API) readinessSkips(ctx context.Context, id uuid.UUID, in *readiness.Input) error {
	rows, err := a.pool.Query(ctx, `SELECT key, COALESCE(value_text, '') FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='check_skip'
		AND last_seen >= now() - interval '`+readinessWindow+`' ORDER BY key, datname`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s readiness.CheckSkip
		if err := rows.Scan(&s.Check, &s.Reason); err != nil {
			return err
		}
		in.Skips = append(in.Skips, s)
	}
	return rows.Err()
}

func (a *API) readinessFailing(ctx context.Context, id uuid.UUID, in *readiness.Input) error {
	rows, err := a.pool.Query(ctx, `SELECT DISTINCT check_name FROM check_results
		WHERE instance_id=$1 AND last_failed AND last_result_at >= now() - interval '`+readinessWindow+`'
		ORDER BY check_name`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		in.FailingChecks = append(in.FailingChecks, name)
	}
	return rows.Err()
}

func (a *API) readinessHost(ctx context.Context, id uuid.UUID, in *readiness.Input) error {
	return a.pool.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM metrics WHERE tenant_id='default' AND instance_id=$1 AND metric='host_disk_total_bytes'
			AND ts >= now() - make_interval(secs => $2)),
		EXISTS(SELECT 1 FROM metrics WHERE tenant_id='default' AND instance_id=$1 AND metric='host_cpu_count'
			AND ts >= now() - make_interval(secs => $2))`,
		id, readiness.HostSampleWindow.Seconds()).
		Scan(&in.HostSeries.HasDiskTotal, &in.HostSeries.HasCPU)
}

func (a *API) readinessFlags(ctx context.Context, id uuid.UUID, in *readiness.Input) error {
	var raw []byte
	err := a.pool.QueryRow(ctx, `SELECT value_json FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind=$2 AND key=$3
		AND last_seen >= now() - interval '`+readinessWindow+`' ORDER BY last_seen DESC LIMIT 1`,
		id, wire.AgentFlagsFact, wire.AgentFlagsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && len(raw) == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	// A fact this server cannot read leaves every flag unknown: no item rather
	// than a guess.
	var flags wire.AgentFlags
	if json.Unmarshal(raw, &flags) == nil {
		in.Flags = flags
	}
	return nil
}

// readinessDatabases derives the database budget from what the agent
// reported: a database skipped for "db_budget" proves the budget is full, and
// the databases monitored are the budget the agent ran with.
func (a *API) readinessDatabases(ctx context.Context, id uuid.UUID, in *readiness.Input) error {
	var monitored, overBudget int
	err := a.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE monitored),
		count(*) FILTER (WHERE NOT monitored AND skip_reason = 'db_budget')
		FROM databases WHERE instance_id=$1`, id).Scan(&monitored, &overBudget)
	if err != nil {
		return err
	}
	if overBudget > 0 {
		in.DatabasesBudget = monitored
		in.DatabasesDiscovered = monitored + overBudget
	}
	return nil
}
