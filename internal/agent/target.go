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
	mu          sync.RWMutex
}

// poolHolder owns the shared pool for a target and its DSN.
// Used internally by Manager.
type poolHolder struct {
	name  string
	pool  *pgxpool.Pool
	dsn   string
	cache instanceIDCache
}
