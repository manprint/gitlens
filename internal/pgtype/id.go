// Package pgtype provides core value types for pglens: ClusterID, InstanceID, Role, Metric, and version handling.
package pgtype

import (
	"hash/fnv"
	"strconv"

	"github.com/google/uuid"
)

// ClusterID is a PostgreSQL system_identifier, or a hash of a user-supplied
// cluster name when the identifier cannot be read. It is stable across
// failover, promote, rename and IP change; that stability is invariant I-1.
type ClusterID uint64

// IDSource indicates how a ClusterID was derived.
type IDSource string

const (
	IDSourceSystemIdentifier IDSource = "system_identifier"
	IDSourceManual           IDSource = "manual"
)

// String renders the identifier in decimal. It is transported as a JSON
// string, never a JSON number: uint64 exceeds the exact integer range of
// IEEE-754 doubles and would be silently rounded by any JSON consumer
// (decision D18).
func (c ClusterID) String() string {
	return strconv.FormatUint(uint64(c), 10)
}

// ParseClusterID parses a decimal ClusterID string.
func ParseClusterID(s string) (ClusterID, error) {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return ClusterID(v), nil
}

// ManualClusterID derives a stable ClusterID from a user-supplied name using
// FNV-1a 64. Two agents given the same cluster name always agree.
func ManualClusterID(name string) ClusterID {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return ClusterID(h.Sum64())
}

// InstanceID is the stable per-instance identifier.
type InstanceID = uuid.UUID

// AgentID is the per-agent identifier assigned at enrollment.
type AgentID = uuid.UUID
