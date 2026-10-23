package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
)

// TestNextGate pins the rules that decide when the schedule is rebuilt: a
// change to a gate rebuilds it, an unreadable target keeps the gate it had, and
// the database set only grows.
func TestNextGate(t *testing.T) {
	primary := targetGate{
		role:      pgtype.RolePrimary,
		version:   pgtype.PGVersion(190000),
		tier:      pgtype.TierReadOnly,
		exts:      map[string]bool{"pg_stat_statements": true},
		databases: map[string]bool{"app": true},
	}
	withDBs := func(g targetGate, names ...string) targetGate {
		g.databases = map[string]bool{}
		for _, n := range names {
			g.databases[n] = true
		}
		return g
	}

	tests := []struct {
		name        string
		last        targetGate
		seen        bool
		read        targetGate
		readOK      bool
		wantChanged bool
		wantDBs     []string
	}{
		{
			name:        "first read is always a change",
			seen:        false,
			read:        primary,
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:        "unreadable target keeps the gate it was scheduled against",
			last:        primary,
			seen:        true,
			read:        withDBs(primary, "other"),
			readOK:      false,
			wantChanged: false,
			wantDBs:     []string{"app"},
		},
		{
			name:        "same gate and databases is no change",
			last:        primary,
			seen:        true,
			read:        primary,
			readOK:      true,
			wantChanged: false,
			wantDBs:     []string{"app"},
		},
		{
			name:        "a promotion changes the role",
			last:        primary,
			seen:        true,
			read:        func() targetGate { g := primary; g.role = pgtype.RoleStandby; return g }(),
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:        "an extension installed later changes the gate",
			last:        primary,
			seen:        true,
			read:        func() targetGate { g := primary; g.exts = map[string]bool{"pg_stat_statements": false}; return g }(),
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:        "a new database is a change and is kept",
			last:        primary,
			seen:        true,
			read:        withDBs(primary, "app", "created_later"),
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app", "created_later"},
		},
		{
			name:        "a database the budget pushed out is still collected and is no change",
			last:        withDBs(primary, "app", "old"),
			seen:        true,
			read:        primary,
			readOK:      true,
			wantChanged: false,
			wantDBs:     []string{"app", "old"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := nextGate(tt.last, tt.seen, tt.read, tt.readOK)
			require.Equal(t, tt.wantChanged, changed)
			var dbs []string
			for name := range got.databases {
				dbs = append(dbs, name)
			}
			require.ElementsMatch(t, tt.wantDBs, dbs)
		})
	}
}
