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
	StatsReset *time.Time
	Truncated  bool
	QueryTexts map[int64]string
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

// ApplySessionLimits sets per-check timeouts via SET LOCAL.
func ApplySessionLimits(ctx context.Context, conn Conn, timeout time.Duration) error {
	ms := int(timeout / time.Millisecond)
	if _, err := conn.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", ms)); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "SET LOCAL lock_timeout = '1s'"); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "SET LOCAL idle_in_transaction_session_timeout = '30s'"); err != nil {
		return err
	}
	return nil
}
