package agent

import (
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
)

// instanceIDCache caches values queried from the database to avoid repeated queries.
type instanceIDCache struct {
	instanceID  pgtype.InstanceID
	agentID     uuid.UUID
	clusterID   pgtype.ClusterID
	role        pgtype.Role
	pgVersion   pgtype.PGVersion
	permTier    pgtype.PermTier
	extensions  map[string]bool
	initialized bool
	// mu guards the fields above. It is never held across network I/O: see
	// Manager.ensureCache.
	mu sync.RWMutex
	// initMu serializes the one-time initialization itself, so concurrent
	// first callers do the discovery queries once rather than N times,
	// without any of them holding mu (and therefore blocking every
	// accessor) for the duration of those queries.
	initMu sync.Mutex
}

// poolHolder owns the shared pool for a target and its DSN.
// Used internally by Manager.
type poolHolder struct {
	name  string
	pool  *pgxpool.Pool
	dsn   string
	cache instanceIDCache
}
