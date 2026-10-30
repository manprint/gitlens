package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/advisor"
)

// Advisor rules and observation windows (11.4, D18, D22c). A rule can be
// disabled from the console: the server keeps the override in
// advisor_rule_overrides and hands it to the advisor engine through
// advisor.OverridesSource; the engine skips the rule and resolves its findings
// at its next evaluation. The two observation windows are stored in
// server_settings and reach the engine the same way, through
// advisor.WindowsSource (13.2); the engine reads them again every 30 seconds.

// advisorWindow is one observation window the console sets.
type advisorWindow struct {
	Key string
}

// advisorWindows is the whitelist of the window keys, in the order the API
// lists them. Any other key, another window.* included, is refused (D22).
var advisorWindows = []advisorWindow{
	{Key: "window.unused_index"},
	{Key: "window.backup_history"},
}

const (
	// advisorWindowDefaultSeconds is the window the advisor uses without a
	// stored value: 7 days.
	advisorWindowDefaultSeconds = 7 * secondsPerDay
	// advisorWindowMinSeconds and advisorWindowMaxSeconds bound a stored
	// window: one hour to 90 days.
	advisorWindowMinSeconds = 3600
	advisorWindowMaxSeconds = 90 * secondsPerDay
)

func isAdvisorWindowKey(key string) bool {
	return slices.ContainsFunc(advisorWindows, func(w advisorWindow) bool { return w.Key == key })
}

func advisorWindowKeys() []string {
	out := make([]string, 0, len(advisorWindows))
	for _, w := range advisorWindows {
		out = append(out, w.Key)
	}
	return out
}

// advisorSettingsStore is the database side of the advisor settings. The
// handlers validate; the store reads and writes the two tables. Unit tests
// stub it.
type advisorSettingsStore interface {
	// RuleOverrides answers the override rows: rule id to enabled.
	RuleOverrides(ctx context.Context) (map[string]bool, error)
	// SetRuleOverride stores enabled false as a row and removes the row for
	// enabled true, the default.
	SetRuleOverride(ctx context.Context, ruleID string, enabled bool) error
	// Windows answers the stored windows, key to seconds. A key without a row,
	// or with a value that is not a valid window, is absent.
	Windows(ctx context.Context) (map[string]int64, error)
	// SetWindows stores set and removes reset in one transaction.
	SetWindows(ctx context.Context, set map[string]int64, reset []string) error
}

// The SQL of the advisor settings store. Every value is a bound parameter; the
// static test ADVW-002 guards that no statement is built from strings.
const (
	advisorOverridesSQL      = `SELECT rule_id, enabled FROM advisor_rule_overrides WHERE tenant_id = 'default'`
	advisorOverrideUpsertSQL = `INSERT INTO advisor_rule_overrides (tenant_id, rule_id, enabled, updated_at) VALUES ('default', $1, $2, now())
		ON CONFLICT (tenant_id, rule_id) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now()`
	advisorOverrideDeleteSQL = `DELETE FROM advisor_rule_overrides WHERE tenant_id = 'default' AND rule_id = $1`
	advisorWindowsSQL        = `SELECT key, value FROM server_settings WHERE tenant_id = 'default' AND key = ANY($1::text[])`
	advisorWindowUpsertSQL   = `INSERT INTO server_settings (tenant_id, key, value, updated_at) VALUES ('default', $1, to_jsonb($2::bigint), now())
		ON CONFLICT (tenant_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`
	advisorWindowDeleteSQL = `DELETE FROM server_settings WHERE tenant_id = 'default' AND key = $1`
)

// pgAdvisorSettingsStore is the advisorSettingsStore of a database.
type pgAdvisorSettingsStore struct {
	pool dbPool
}

func (s pgAdvisorSettingsStore) RuleOverrides(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, advisorOverridesSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var (
			id      string
			enabled bool
		)
		if err := rows.Scan(&id, &enabled); err != nil {
			return nil, err
		}
		out[id] = enabled
	}
	return out, rows.Err()
}

func (s pgAdvisorSettingsStore) SetRuleOverride(ctx context.Context, ruleID string, enabled bool) error {
	if enabled {
		_, err := s.pool.Exec(ctx, advisorOverrideDeleteSQL, ruleID)
		return err
	}
	_, err := s.pool.Exec(ctx, advisorOverrideUpsertSQL, ruleID, false)
	return err
}

func (s pgAdvisorSettingsStore) Windows(ctx context.Context) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx, advisorWindowsSQL, advisorWindowKeys())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var (
			key   string
			value []byte
		)
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		seconds, err := parseWindowSeconds(value)
		if err != nil || !windowInBounds(seconds) {
			// A value written outside the console is not a window: the
			// default applies, and a PUT replaces it.
			log.Printf("advisor windows: ignoring the stored %s (%s): not an integer between %d and %d seconds", key, value, advisorWindowMinSeconds, advisorWindowMaxSeconds)
			continue
		}
		out[key] = seconds
	}
	return out, rows.Err()
}

func (s pgAdvisorSettingsStore) SetWindows(ctx context.Context, set map[string]int64, reset []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	// A no-op after Commit; on any error no key changes.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	for _, key := range advisorWindowKeys() {
		if seconds, ok := set[key]; ok {
			if _, err := tx.Exec(ctx, advisorWindowUpsertSQL, key, seconds); err != nil {
				return fmt.Errorf("store %s: %w", key, err)
			}
		}
		if slices.Contains(reset, key) {
			if _, err := tx.Exec(ctx, advisorWindowDeleteSQL, key); err != nil {
				return fmt.Errorf("reset %s: %w", key, err)
			}
		}
	}
	return tx.Commit(ctx)
}

// AdvisorOverridesSource is the advisor engine's view of the console's rule
// overrides (advisor.OverridesSource). Without a pool no rule is disabled.
func AdvisorOverridesSource(pool *pgxpool.Pool) advisor.OverridesSource {
	if pool == nil {
		return nil
	}
	st := pgAdvisorSettingsStore{pool: pool}
	return st.RuleOverrides
}

// AdvisorWindowsSource is the advisor engine's view of the console's
// observation windows (advisor.WindowsSource). Without a pool the engine keeps
// its defaults.
func AdvisorWindowsSource(pool *pgxpool.Pool) advisor.WindowsSource {
	if pool == nil {
		return nil
	}
	return advisorWindowsSource(pgAdvisorSettingsStore{pool: pool})
}

// advisorWindowsSource reads the two window keys through the loader the API
// uses, which drops a stored value outside the bounds, so a key without a
// valid value is its default. A failed read answers the windows of the last
// read that succeeded, the defaults before any: a passing database error must
// not move a window.
func advisorWindowsSource(st advisorSettingsStore) advisor.WindowsSource {
	var mu sync.Mutex
	last := advisor.DefaultWindows()
	return func(ctx context.Context) advisor.Windows {
		stored, err := st.Windows(ctx)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			log.Printf("advisor windows: read the console's windows (keeping the previous ones): %v", err)
			return last
		}
		last = advisorWindowsOf(stored)
		return last
	}
}

// advisorWindowsOf turns the stored windows, key to seconds, into the
// advisor's. A key that is absent or out of bounds is its default.
func advisorWindowsOf(stored map[string]int64) advisor.Windows {
	windows := advisor.DefaultWindows()
	window := func(key string, def time.Duration) time.Duration {
		if seconds, ok := stored[key]; ok && windowInBounds(seconds) {
			return time.Duration(seconds) * time.Second
		}
		return def
	}
	windows.UnusedIndex = window("window.unused_index", windows.UnusedIndex)
	windows.BackupHistory = window("window.backup_history", windows.BackupHistory)
	return windows
}

// advisorSettings is the store of the API: the stub of a test, else the
// database, else nil (no store).
func (a *API) advisorSettingsStore() advisorSettingsStore {
	if a.advisorSettings != nil {
		return a.advisorSettings
	}
	if a.pool == nil {
		return nil
	}
	return pgAdvisorSettingsStore{pool: a.pool}
}

func (a *API) registerAdvisorSettingsRoutes(r chi.Router) {
	r.Put("/api/v1/advisor/rules/{id}/override", a.handleSetAdvisorRuleOverride)
	r.Get("/api/v1/settings/advisor-windows", a.handleGetAdvisorWindows)
	r.Put("/api/v1/settings/advisor-windows", a.handleSetAdvisorWindows)
}

// advisorRuleJSON is one rule as getAdvisorRules lists it (AdvisorRule).
type advisorRuleJSON struct {
	ID       string           `json:"id"`
	Severity advisor.Severity `json:"severity"`
	Scope    advisor.Scope    `json:"scope"`
	Needs    []string         `json:"needs"`
	MinTier  string           `json:"min_tier"`
	// Enabled is false when the console disabled the rule.
	Enabled bool `json:"enabled"`
	// Overridden is true when the rule has an override row.
	Overridden bool `json:"overridden"`
}

func renderAdvisorRule(rule advisor.Rule, overrides map[string]bool) advisorRuleJSON {
	enabled, overridden := overrides[rule.ID()]
	if !overridden {
		enabled = true
	}
	return advisorRuleJSON{
		ID: rule.ID(), Severity: rule.Severity(), Scope: rule.Scope(), Needs: rule.Needs(),
		MinTier: rule.MinTier().String(), Enabled: enabled, Overridden: overridden,
	}
}

func lookupAdvisorRule(id string) (advisor.Rule, bool) {
	for _, rule := range advisor.All() {
		if rule.ID() == id {
			return rule, true
		}
	}
	return nil, false
}

// advisorOverrides reads the overrides, or none without a store (C5).
func (a *API) advisorOverrides(ctx context.Context) (map[string]bool, error) {
	st := a.advisorSettingsStore()
	if st == nil {
		return map[string]bool{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return st.RuleOverrides(ctx)
}

// listAdvisorRules answers the rule catalogue with each rule's enablement.
// Without a database every rule is enabled; an unreadable override table is a
// 503 (R-11.0 C5), since an "enabled" read from nothing would be a guess.
func (a *API) listAdvisorRules(w http.ResponseWriter, r *http.Request) {
	overrides, err := a.advisorOverrides(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", err.Error())
		return
	}
	rules := advisor.All()
	out := make([]advisorRuleJSON, 0, len(rules))
	for _, rule := range rules {
		out = append(out, renderAdvisorRule(rule, overrides))
	}
	writeJSON(w, http.StatusOK, out)
}

type advisorRuleOverrideBody struct {
	Enabled *bool `json:"enabled"`
}

// handleSetAdvisorRuleOverride enables or disables a rule. Disabling stores a
// row; enabling deletes it, back to the default. The engine applies the change
// at its next evaluation.
func (a *API) handleSetAdvisorRuleOverride(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rule, ok := lookupAdvisorRule(id)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown rule", id)
		return
	}
	var in advisorRuleOverrideBody
	if !decodeStrictJSONBody(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid enabled", "enabled is required and must be a boolean")
		return
	}
	st := a.advisorSettingsStore()
	if st == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := st.SetRuleOverride(ctx, id, *in.Enabled); err != nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", err.Error())
		return
	}
	state := "enabled"
	if !*in.Enabled {
		state = "disabled"
	}
	log.Printf("advisor: rule %s %s by %s", id, state, requesterOf(r, a.auth))
	overrides := map[string]bool{}
	if !*in.Enabled {
		overrides[id] = false
	}
	writeJSON(w, http.StatusOK, renderAdvisorRule(rule, overrides))
}

// advisorWindowJSON is one window as getAdvisorWindows lists it.
type advisorWindowJSON struct {
	Key            string `json:"key"`
	Seconds        int64  `json:"seconds"`
	DefaultSeconds int64  `json:"default_seconds"`
	MinSeconds     int64  `json:"min_seconds"`
	MaxSeconds     int64  `json:"max_seconds"`
	Source         string `json:"source"`
}

func renderAdvisorWindows(stored map[string]int64) []advisorWindowJSON {
	out := make([]advisorWindowJSON, 0, len(advisorWindows))
	for _, window := range advisorWindows {
		row := advisorWindowJSON{
			Key: window.Key, Seconds: advisorWindowDefaultSeconds, DefaultSeconds: advisorWindowDefaultSeconds,
			MinSeconds: advisorWindowMinSeconds, MaxSeconds: advisorWindowMaxSeconds, Source: "default",
		}
		if seconds, ok := stored[window.Key]; ok {
			row.Seconds, row.Source = seconds, "custom"
		}
		out = append(out, row)
	}
	return out
}

func windowInBounds(seconds int64) bool {
	return seconds >= advisorWindowMinSeconds && seconds <= advisorWindowMaxSeconds
}

// parseWindowSeconds reads a JSON integer. A fraction, an exponent or any
// other JSON value is refused.
func parseWindowSeconds(raw []byte) (int64, error) {
	return strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
}

func (a *API) readAdvisorWindows(ctx context.Context, st advisorSettingsStore) ([]advisorWindowJSON, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stored, err := st.Windows(ctx)
	if err != nil {
		return nil, err
	}
	return renderAdvisorWindows(stored), nil
}

// handleGetAdvisorWindows lists both windows in their fixed order.
func (a *API) handleGetAdvisorWindows(w http.ResponseWriter, r *http.Request) {
	st := a.advisorSettingsStore()
	if st == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	out, err := a.readAdvisorWindows(r.Context(), st)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSetAdvisorWindows sets (a number of seconds) or resets (null) each key
// of the body, all or none. An empty object changes nothing.
func (a *API) handleSetAdvisorWindows(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if !decodeJSONBody(w, r, &raw) {
		return
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		writeError(w, http.StatusBadRequest, "invalid body", "the body must be an object of window keys")
		return
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body", err.Error())
		return
	}
	set := map[string]int64{}
	var reset []string
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if !isAdvisorWindowKey(key) {
			writeError(w, http.StatusBadRequest, "unknown key",
				fmt.Sprintf("%s is not a setting; the keys are %s", key, strings.Join(advisorWindowKeys(), " and ")))
			return
		}
		value := bytes.TrimSpace(body[key])
		if string(value) == "null" {
			reset = append(reset, key)
			continue
		}
		seconds, err := parseWindowSeconds(value)
		if err != nil || !windowInBounds(seconds) {
			writeError(w, http.StatusBadRequest, "invalid seconds",
				fmt.Sprintf("%s must be an integer number of seconds between %d and %d, or null for the default", key, advisorWindowMinSeconds, advisorWindowMaxSeconds))
			return
		}
		set[key] = seconds
	}
	st := a.advisorSettingsStore()
	if st == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	if len(set) > 0 || len(reset) > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		err := st.SetWindows(ctx, set, reset)
		cancel()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "store unavailable", err.Error())
			return
		}
		log.Printf("advisor windows: %s by %s", describeWindowChange(set, reset), requesterOf(r, a.auth))
	}
	out, err := a.readAdvisorWindows(r.Context(), st)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func describeWindowChange(set map[string]int64, reset []string) string {
	parts := make([]string, 0, len(set)+len(reset))
	for _, key := range advisorWindowKeys() {
		if seconds, ok := set[key]; ok {
			parts = append(parts, fmt.Sprintf("%s set to %d s", key, seconds))
		}
		if slices.Contains(reset, key) {
			parts = append(parts, key+" reset to the default")
		}
	}
	return strings.Join(parts, ", ")
}
