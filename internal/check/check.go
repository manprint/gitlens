package check

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

// Scope defines the scope of a check.
type Scope string

const (
	ScopeInstance Scope = "instance"
	ScopeDatabase Scope = "database"
	ScopeCluster  Scope = "cluster"
)

// Requirements defines what a check needs to run.
type Requirements struct {
	Roles      []pgtype.Role    `json:"roles"`
	MinPG      pgtype.PGVersion `json:"min_pg"`
	MaxPG      pgtype.PGVersion `json:"max_pg"`
	Extensions []string         `json:"extensions"`
	PermTier   pgtype.PermTier  `json:"perm_tier"`
	Scope      Scope            `json:"scope"`
}

// Supports reports whether the check can run against this target, and why not when it cannot.
func (r Requirements) Supports(role pgtype.Role, v pgtype.PGVersion, tier pgtype.PermTier, exts map[string]bool) (bool, string) {
	// Role check: nil means any role
	if len(r.Roles) > 0 {
		found := false
		for _, allowed := range r.Roles {
			if allowed == role {
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Sprintf("role %s not allowed", role)
		}
	}
	// Version checks: 0 means no bound
	if r.MinPG != 0 && v < r.MinPG {
		return false, fmt.Sprintf("requires PG >= %s, got %s", r.MinPG.String(), v.String())
	}
	if r.MaxPG != 0 && v > r.MaxPG {
		return false, fmt.Sprintf("requires PG <= %s, got %s", r.MaxPG.String(), v.String())
	}
	// Permission tier
	if tier < r.PermTier {
		return false, fmt.Sprintf("requires %s, got %s", r.PermTier.String(), tier.String())
	}
	// Extensions
	for _, ext := range r.Extensions {
		if !exts[ext] {
			return false, fmt.Sprintf("missing extension %s", ext)
		}
	}
	return true, ""
}

// Result is the result of a check scrape.
type Result struct {
	Metrics    []pgtype.Metric
	Facts      []Fact
	StatsReset *time.Time
	// Truncated reports that a hard cardinality cap (cardinality.Options'
	// MaxKeys) dropped keys the selection wanted to report: the configured
	// budget is too small for this workload, which is worth an alert.
	Truncated bool
	// Partial reports that the check deliberately reported a top-N subset of
	// the objects it saw. It is the normal state of any real database, so it
	// only tells the UI that the view is not exhaustive and tells the server
	// that objects missing from this result were not observed rather than
	// gone. It never means the budget was exceeded.
	Partial    bool
	QueryTexts map[int64]string
}

// Fact is the check-local representation of a non-numeric observation.
type Fact struct {
	Kind      string
	Key       string
	Labels    map[string]string
	ValueText string
	ValueJSON []byte
}

// Check is the interface for a monitoring check.
type Check interface {
	Name() string
	Requires() Requirements
	DefaultInterval() time.Duration
	Timeout() time.Duration
	Scrape(ctx context.Context, t Target) (Result, error)
}

// TopNConfigurable is implemented by checks that expose a bounded top-N
// selection policy through agent configuration.
type TopNConfigurable interface {
	SetTopN(topN int)
}

// Conn is the subset of *pgxpool.Conn a check needs. Narrowing the Target
// interface to this instead of the concrete pgxpool type lets Scrape() be
// exercised in unit tests against a mock, without a real database.
type Conn interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Release()
}

// Target is everything a check may know about what it is scraping.
type Target interface {
	InstanceID() pgtype.InstanceID
	ClusterID() pgtype.ClusterID
	Role() pgtype.Role
	PGVersion() pgtype.PGVersion
	PermTier() pgtype.PermTier
	HasExtension(name string) bool
	Conn(ctx context.Context) (Conn, error)
	ConnFor(ctx context.Context, datname string) (Conn, error)
	Database() string
	Clock() clock.Clock
}

// ApplySessionLimits sets per-check timeouts on the session with plain SET
// statements. A check's Conn exposes no transaction, and SET LOCAL outside one
// only draws a WARNING and changes nothing. The limits therefore stay on a
// pooled connection after the check returns, and the agent reapplies them
// when it hands a pooled connection out again.
func ApplySessionLimits(ctx context.Context, conn Conn, timeout time.Duration) error {
	ms := int(timeout / time.Millisecond)
	if _, err := conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = %d", ms)); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "SET lock_timeout = '1s'"); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "SET idle_in_transaction_session_timeout = '30s'"); err != nil {
		return err
	}
	return nil
}
