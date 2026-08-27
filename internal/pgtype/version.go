package pgtype

import "fmt"

// PGVersion is server_version_num, e.g. 160004 for PostgreSQL 16.4.
type PGVersion int

// Major returns the major version, e.g. 16 for 160004.
func (v PGVersion) Major() int {
	return int(v) / 10000
}

// Minor returns the minor version, e.g. 4 for 160004.
func (v PGVersion) Minor() int {
	return int(v) % 100
}

// String returns "major.minor", e.g. "16.4".
func (v PGVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major(), v.Minor())
}

// Supported reports whether this project supports the version at all
// (decision D11: PostgreSQL 15 through 18 inclusive).
func (v PGVersion) Supported() bool {
	major := v.Major()
	return major >= 15 && major < 19
}

const (
	PG15 PGVersion = 150000
	PG16 PGVersion = 160000
	PG17 PGVersion = 170000
	PG18 PGVersion = 180000
	PG19 PGVersion = 190000 // exclusive upper bound
)
