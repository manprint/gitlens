//go:build integration

package main

import (
	"context"
	"net"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
)

// TestINTID001_HostInQueryDSNIdentifiesTarget gives the agent the same server
// with its host and port moved out of the authority into the query, the form
// a Unix-socket DSN takes. The target must still get an instance id, a
// version and a role, not the zero values an unfingerprintable DSN produced.
func TestINTID001_HostInQueryDSNIdentifiesTarget(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		dsn, err := url.Parse(pg.DSN("postgres", pgtest.RoleT0))
		require.NoError(t, err)
		host, port, err := net.SplitHostPort(dsn.Host)
		require.NoError(t, err)
		query := dsn.Query()
		query.Set("host", host)
		query.Set("port", port)
		dsn.Host = ""
		dsn.RawQuery = query.Encode()

		mgr, err := agent.NewManager(ctx, "host-in-query", dsn.String(), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)
		defer mgr.Close()

		require.NotEqual(t, pgtype.InstanceID{}, mgr.InstanceID(), "the target has an instance id")
		require.NotEqual(t, uuid.Nil, mgr.AgentID(), "the agent has an id")
		require.Equal(t, pg.Version, mgr.PGVersion())
		require.Equal(t, pgtype.RolePrimary, mgr.Role())
	})
}
