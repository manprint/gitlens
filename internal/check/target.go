package check

import (
	"context"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

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
	DatabaseValue   string
	ClockValue      clock.Clock
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
func (t *SimpleTarget) Database() string   { return t.DatabaseValue }
func (t *SimpleTarget) Clock() clock.Clock { return t.ClockValue }
