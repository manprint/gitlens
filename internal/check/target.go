package check

import (
	"context"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

// DatabaseMonitor is implemented by a target that knows which databases the
// agent collects from. A check that opens a connection to another database by
// name must ask first: the connection manager opens a pool for any name it is
// given, and a database the agent does not collect from must not be read.
type DatabaseMonitor interface {
	MonitorsDatabase(datname string) bool
}

// monitorsDatabase reports whether t collects from datname. A target that does
// not say so is taken to collect from none, so a check reads no database it
// cannot confirm.
func monitorsDatabase(t Target, datname string) bool {
	m, ok := t.(DatabaseMonitor)
	return ok && m.MonitorsDatabase(datname)
}

// SimpleTarget is a testable implementation of Target.
type SimpleTarget struct {
	InstanceIDValue pgtype.InstanceID
	ClusterIDValue  pgtype.ClusterID
	RoleValue       pgtype.Role
	PGVersionValue  pgtype.PGVersion
	PermTierValue   pgtype.PermTier
	Extensions      map[string]bool
	ConnFunc        func(ctx context.Context) (Conn, error)
	ConnForFunc     func(ctx context.Context, datname string) (Conn, error)
	// Monitored lists the databases MonitorsDatabase accepts. Nil accepts none.
	Monitored     map[string]bool
	DatabaseValue string
	ClockValue    clock.Clock
}

func (t *SimpleTarget) InstanceID() pgtype.InstanceID { return t.InstanceIDValue }
func (t *SimpleTarget) ClusterID() pgtype.ClusterID   { return t.ClusterIDValue }
func (t *SimpleTarget) Role() pgtype.Role             { return t.RoleValue }
func (t *SimpleTarget) PGVersion() pgtype.PGVersion   { return t.PGVersionValue }
func (t *SimpleTarget) PermTier() pgtype.PermTier     { return t.PermTierValue }
func (t *SimpleTarget) HasExtension(name string) bool {
	if t.Extensions == nil {
		return false
	}
	return t.Extensions[name]
}
func (t *SimpleTarget) Conn(ctx context.Context) (Conn, error) {
	if t.ConnFunc != nil {
		return t.ConnFunc(ctx)
	}
	return nil, nil
}
func (t *SimpleTarget) ConnFor(ctx context.Context, datname string) (Conn, error) {
	if t.ConnForFunc != nil {
		return t.ConnForFunc(ctx, datname)
	}
	return t.Conn(ctx)
}
func (t *SimpleTarget) MonitorsDatabase(datname string) bool { return t.Monitored[datname] }
func (t *SimpleTarget) Database() string                     { return t.DatabaseValue }
func (t *SimpleTarget) Clock() clock.Clock                   { return t.ClockValue }
