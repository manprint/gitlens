package store

import "github.com/manprint/pglens/internal/pgtype"

// ToDB converts a ClusterID to the bigint stored in PostgreSQL.
// PostgreSQL has no unsigned 64-bit type, so the value is stored as the
// two's-complement bit pattern of the uint64. Values above 2^63 display as
// negative in psql, which is expected.
func ToDB(id pgtype.ClusterID) int64 {
	return int64(id)
}

// FromDB converts a bigint from PostgreSQL to a ClusterID.
func FromDB(v int64) pgtype.ClusterID {
	return pgtype.ClusterID(uint64(v))
}
