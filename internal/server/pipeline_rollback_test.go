package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

// The delta and topology engines learn from an envelope while Process runs,
// before the envelope's rows commit. When the commit fails, the envelope is
// retried, and the retry must see the engines as they were before the failed
// attempt: otherwise the retry loses the rate or the event the first attempt
// would have written.

var rollbackT0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// counterEnvelope reports one cumulative counter for one instance, as a
// primary, at the given fake-clock time.
func counterEnvelope(instID uuid.UUID, cid string, ts time.Time, value float64) wire.Envelope {
	return wire.Envelope{Instances: []wire.Instance{{
		InstanceID: instID.String(),
		ClusterID:  cid,
		Role:       "primary",
		Results: []wire.Result{{Check: "bgwriter", TS: ts, Metrics: []wire.Metric{
			{Name: "pg_xact_commit", Value: value, Kind: "counter", Labels: map[string]string{}},
		}}},
	}}}
}

// metricValues returns the values written for one metric by the batched metric
// inserts of a transaction.
func metricValues(tx *recordingTx, metric string) []float64 {
	var values []float64
	for _, e := range tx.execs {
		if !strings.Contains(e.sql, "INSERT INTO metrics (") {
			continue
		}
		if e.args[5] == metric {
			values = append(values, e.args[8].(float64))
		}
	}
	return values
}

// eventTypesFor returns the types of the events a transaction wrote for one
// instance.
func eventTypesFor(tx *recordingTx, instance uuid.UUID) []string {
	var types []string
	for _, e := range tx.execs {
		if !strings.Contains(e.sql, "INSERT INTO events (") {
			continue
		}
		if id, ok := e.args[4].(*uuid.UUID); ok && id != nil && *id == instance {
			types = append(types, e.args[2].(string))
		}
	}
	return types
}

// topologyWritesFor counts the statements a transaction ran on the topology
// edges of one instance.
func topologyWritesFor(tx *recordingTx, instance uuid.UUID) int {
	n := 0
	for _, e := range tx.execs {
		if !strings.Contains(e.sql, "topology_edges") {
			continue
		}
		if id, ok := e.args[2].(uuid.UUID); ok && id == instance {
			n++
		}
	}
	return n
}

// processOn runs one envelope through a transaction that fails at commit when
// commitErr is set, and returns that transaction.
func processOn(t *testing.T, p *Pipeline, env wire.Envelope, commitErr error) (*recordingTx, error) {
	t.Helper()
	tx := &recordingTx{mockTx: mockTx{commitErr: commitErr}}
	p.pool = &mockPool{beginTx: tx}
	_, err := p.Process(context.Background(), env)
	return tx, err
}

// TestPipeline_RetryAfterCommitFailureRecomputesRate: the first attempt at the
// sample observes it and then fails to commit. The retry must still derive the
// rate from the baseline that was committed, not from the failed sample.
func TestPipeline_RetryAfterCommitFailureRecomputesRate(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(rollbackT0)
	p := NewPipeline(nil, fc)
	instID := uuid.New()
	cid := pgtype.ClusterID(1).String()

	_, err := processOn(t, p, counterEnvelope(instID, cid, rollbackT0, 100), nil)
	require.NoError(t, err)

	sample := counterEnvelope(instID, cid, rollbackT0.Add(60*time.Second), 150)
	_, err = processOn(t, p, sample, errors.New("commit lost"))
	require.ErrorContains(t, err, "commit tx")

	retry, err := processOn(t, p, sample, nil)
	require.NoError(t, err)
	values := metricValues(retry, "pg_xact_commit")
	require.Len(t, values, 1, "the retried sample must produce its rate row")
	require.InDelta(t, 50.0/60.0, values[0], 1e-9)
}

// TestPipeline_RetryAfterCommitFailureRepeatsFailover: the first attempt sees
// the standby promoted and fails to commit. The retry must still report the
// failover, which needs the prior primary the failed attempt had overwritten.
func TestPipeline_RetryAfterCommitFailureRepeatsFailover(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(rollbackT0)
	p := NewPipeline(nil, fc)
	cid := pgtype.ClusterID(1).String()
	oldPrimary := uuid.New()
	standby := uuid.New()

	_, err := processOn(t, p, wire.Envelope{Instances: []wire.Instance{
		{InstanceID: oldPrimary.String(), ClusterID: cid, Role: "primary", Addr: "10.0.0.1", Port: 5432},
		{InstanceID: standby.String(), ClusterID: cid, Role: "standby", Addr: "10.0.0.2", Port: 5432},
	}}, nil)
	require.NoError(t, err)

	fc.Advance(10 * time.Second)
	promoted := wire.Envelope{Instances: []wire.Instance{
		{InstanceID: standby.String(), ClusterID: cid, Role: "primary", Addr: "10.0.0.2", Port: 5432},
	}}
	_, err = processOn(t, p, promoted, errors.New("commit lost"))
	require.ErrorContains(t, err, "commit tx")

	retry, err := processOn(t, p, promoted, nil)
	require.NoError(t, err)
	require.Contains(t, eventTypesFor(retry, standby), "failover_detected")
}

// TestPipeline_RejectedInstanceChangesNoTopologyState: an instance the pipeline
// rejects writes no topology edges, and the topology engine forgets what it
// learned from it, so its next accepted envelope is a first sighting again.
func TestPipeline_RejectedInstanceChangesNoTopologyState(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(rollbackT0)
	p := NewPipeline(nil, fc)
	cid := pgtype.ClusterID(1).String()
	standby := uuid.New()
	withMetric := func(kind string) wire.Envelope {
		return wire.Envelope{Instances: []wire.Instance{{
			InstanceID:    standby.String(),
			ClusterID:     cid,
			Role:          "standby",
			TopologyEdges: []wire.Edge{{From: standby.String(), To: "pg-unresolved", Type: "streaming", Confidence: "high"}},
			Results: []wire.Result{{Check: "bgwriter", TS: rollbackT0, Metrics: []wire.Metric{
				{Name: "pg_checkpoints_timed", Value: 1, Kind: kind, Labels: map[string]string{}},
			}}},
		}}}
	}

	rejected, err := processOn(t, p, withMetric("unknown-kind"), nil)
	require.NoError(t, err)
	require.Zero(t, topologyWritesFor(rejected, standby), "a rejected instance must write no topology edges")

	accepted, err := processOn(t, p, withMetric("gauge"), nil)
	require.NoError(t, err)
	require.Contains(t, eventTypesFor(accepted, standby), "role_change", "the first accepted sighting is a first sighting")
}

// TestPipeline_LastSeenOnlyForStoredInstances: the server reads last_seen as
// the instance being up. It is refreshed in the envelope's transaction for an
// instance whose samples are stored, and not for one the pipeline refuses,
// which must go stale rather than read as up on every push with no data.
// An instance that reports no results in an envelope is still accepted, and
// its last_seen is refreshed: the refresh is per accepted instance, not per
// sample, so a quiet but healthy instance stays up.
func TestPipeline_LastSeenRefreshedForInstanceWithoutResults(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(rollbackT0)
	p := NewPipeline(nil, fc)
	quiet := uuid.New()
	tx, err := processOn(t, p, wire.Envelope{Instances: []wire.Instance{
		{InstanceID: quiet.String(), ClusterID: pgtype.ClusterID(1).String(), Role: "primary"},
	}}, nil)
	require.NoError(t, err)

	var touched []uuid.UUID
	for _, e := range tx.execs {
		if strings.Contains(e.sql, "UPDATE instances SET last_seen") {
			touched = e.args[0].([]uuid.UUID)
		}
	}
	require.Equal(t, []uuid.UUID{quiet}, touched)
}

func TestPipeline_LastSeenOnlyForStoredInstances(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(rollbackT0)
	p := NewPipeline(nil, fc)
	cid := pgtype.ClusterID(1).String()
	stored, refused := uuid.New(), uuid.New()
	gauge := func(kind string) []wire.Result {
		return []wire.Result{{Check: "bgwriter", TS: rollbackT0, Metrics: []wire.Metric{
			{Name: "pg_checkpoints_timed", Value: 1, Kind: kind, Labels: map[string]string{}},
		}}}
	}
	tx, err := processOn(t, p, wire.Envelope{Instances: []wire.Instance{
		{InstanceID: stored.String(), ClusterID: cid, Role: "primary", Results: gauge("gauge")},
		{InstanceID: refused.String(), ClusterID: cid, Role: "primary", Results: gauge("unknown-kind")},
	}}, nil)
	require.NoError(t, err)

	var touched []uuid.UUID
	for _, e := range tx.execs {
		if strings.Contains(e.sql, "UPDATE instances SET last_seen") {
			touched = e.args[0].([]uuid.UUID)
		}
	}
	require.Equal(t, []uuid.UUID{stored}, touched)
}

// TestPipeline_RoleStoredOnlyForStoredInstances: the role an instance reports is
// stored in the envelope's transaction, with its last_seen, and only for an
// instance whose samples are stored. A refused instance keeps the role it had,
// as its topology state is rolled back with it.
func TestPipeline_RoleStoredOnlyForStoredInstances(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(rollbackT0)
	p := NewPipeline(nil, fc)
	cid := pgtype.ClusterID(1).String()
	stored, refused := uuid.New(), uuid.New()
	gauge := func(kind string) []wire.Result {
		return []wire.Result{{Check: "bgwriter", TS: rollbackT0, Metrics: []wire.Metric{
			{Name: "pg_checkpoints_timed", Value: 1, Kind: kind, Labels: map[string]string{}},
		}}}
	}
	tx, err := processOn(t, p, wire.Envelope{Instances: []wire.Instance{
		{InstanceID: stored.String(), ClusterID: cid, Role: "standby", Results: gauge("gauge")},
		{InstanceID: refused.String(), ClusterID: cid, Role: "primary", Results: gauge("unknown-kind")},
	}}, nil)
	require.NoError(t, err)

	roles := map[uuid.UUID]any{}
	for _, e := range tx.execs {
		if strings.Contains(e.sql, "UPDATE instances SET role") {
			roles[e.args[1].(uuid.UUID)] = e.args[0]
		}
	}
	require.Equal(t, map[uuid.UUID]any{stored: "standby"}, roles)
}
