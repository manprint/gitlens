//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/wire"
)

// TestINTXMIN005_XminHolderFactReplacesAndIsWithheld: the activity check's
// oldest xmin holder is one fact that each complete activity sample replaces, so
// a sample that names no holder removes the one of the previous sample. The
// server withholds a free-text identity that may hold a credential, whatever
// the sender did.
func TestINTXMIN005_XminHolderFactReplacesAndIsWithheld(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	instance := uuid.NewString()
	push := func(ts time.Time, facts ...wire.Fact) {
		env := wire.Envelope{ProtocolVersion: wire.ProtocolVersionCurrent, AgentID: uuid.NewString(), SentAt: ts, Instances: []wire.Instance{{InstanceID: instance, ClusterID: "29", Role: "primary", Results: []wire.Result{{Check: "activity", TS: ts, Facts: facts}}}}}
		assertPush(t, factsRouter(pool), env, http.StatusAccepted)
	}
	count := func() int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM object_facts WHERE instance_id=$1 AND kind='xmin_horizon'`, instance).Scan(&n))
		return n
	}

	t1 := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	push(t1, wire.Fact{Kind: "xmin_horizon", Key: "oldest", ValueJSON: json.RawMessage(`{"holder_kind":"standby","identity":"postgresql://u:p@h/db","age_xids":40}`)})
	require.Equal(t, 1, count())
	var stored string
	require.NoError(t, pool.QueryRow(ctx, `SELECT value_json::text FROM object_facts WHERE instance_id=$1 AND kind='xmin_horizon'`, instance).Scan(&stored))
	require.JSONEq(t, `{"holder_kind":"standby","identity":"[redacted]","age_xids":40}`, stored)

	push(t1.Add(10 * time.Second))
	require.Equal(t, 0, count())
}
