//go:build integration

package wire_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// testTarget is a check.Target for running checks in this integration test.
type testTarget struct {
	conn       *pgxpool.Conn
	version    pgtype.PGVersion
	database   string
	instanceID pgtype.InstanceID
	clusterID  pgtype.ClusterID
	role       pgtype.Role
	permTier   pgtype.PermTier
	extensions map[string]bool
}

func (m *testTarget) InstanceID() pgtype.InstanceID { return m.instanceID }
func (m *testTarget) ClusterID() pgtype.ClusterID   { return m.clusterID }
func (m *testTarget) Role() pgtype.Role             { return m.role }
func (m *testTarget) PGVersion() pgtype.PGVersion   { return m.version }
func (m *testTarget) PermTier() pgtype.PermTier     { return m.permTier }
func (m *testTarget) HasExtension(name string) bool { return m.extensions[name] }
func (m *testTarget) Conn(ctx context.Context) (check.Conn, error) {
	return m.conn, nil
}
func (m *testTarget) ConnFor(ctx context.Context, datname string) (check.Conn, error) {
	return m.conn, nil
}
func (m *testTarget) Database() string {
	return m.database
}
func (m *testTarget) Clock() clock.Clock {
	return clock.System()
}

// normalizeEnvelope zeroes out non-deterministic fields so the envelope
// can be compared across runs. This preserves the structure (which checks
// are present, which metrics they emit, which labels they have) but removes
// identifiers, timestamps, and metric values that vary by run.
func normalizeEnvelope(env *wire.Envelope) *wire.Envelope {
	normalized := *env
	// Zero the timestamp — it's per-run
	normalized.SentAt = time.Time{}
	// Zero the agent ID — it's per-run
	normalized.AgentID = ""

	// Normalize instances
	normalized.Instances = make([]wire.Instance, len(env.Instances))
	for i, inst := range env.Instances {
		normInst := inst
		// Zero out per-run identifiers
		normInst.InstanceID = ""
		normInst.ClusterID = ""
		// Zero address/port (these come from test container config)
		normInst.Addr = ""
		normInst.Port = 0

		// Normalize results
		normInst.Results = make([]wire.Result, len(inst.Results))
		for j, res := range inst.Results {
			normRes := res
			// Zero out timestamps
			normRes.TS = time.Time{}
			// Normalize metrics: keep structure (check name, metric names, label keys)
			// but zero out values and label values, since these vary per run based on
			// database state/identity. The meaningful test is that the schema is
			// present and correct on each version.
			normRes.Metrics = make([]wire.Metric, len(res.Metrics))
			for k, m := range res.Metrics {
				// Preserve label structure (keys) but normalize values
				normLabels := make(map[string]string)
				for lkey := range m.Labels {
					// Keep label keys but zero out values
					normLabels[lkey] = ""
				}
				normRes.Metrics[k] = wire.Metric{
					Name:   m.Name,
					Labels: normLabels,
					Value:  0, // Normalize values to 0
					Kind:   m.Kind,
				}
			}
			normRes.QueryTexts = map[string]string{} // Zero out query texts
			normInst.Results[j] = normRes
		}

		normalized.Instances[i] = normInst
	}

	return &normalized
}

// INT-GOLDEN-001: the normalized envelope matches testdata/payload_pg<major>.json
// for every version in the matrix.
func TestINTGOLDEN001_NormalizedEnvelopeStructure(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)

		ctx := context.Background()
		dataPool := pg.Pool(t, "app", pgtest.RoleT0)

		// Query what extensions are available on this instance
		setupConn, err := dataPool.Acquire(ctx)
		require.NoError(t, err)
		rows, err := setupConn.Query(ctx, `SELECT extname FROM pg_extension`)
		require.NoError(t, err)
		extensions := make(map[string]bool)
		for rows.Next() {
			var ext string
			require.NoError(t, rows.Scan(&ext))
			extensions[ext] = true
		}
		rows.Close()
		setupConn.Release()

		// Build the envelope by collecting results from all checks
		now := time.Now()
		env := &wire.Envelope{
			// INT-GOLDEN-001 is the frozen v1 regression fixture. Keep these
			// documents unchanged while protocol-v2 fixtures are tested separately.
			ProtocolVersion: wire.ProtocolVersionMin,
			AgentID:         uuid.NewString(),
			SentAt:          now,
			Instances: []wire.Instance{
				{
					InstanceID:      uuid.NewString(),
					ClusterID:       "1",
					ClusterIDSource: "manual",
					Addr:            "127.0.0.1",
					Port:            5432,
					PGVersion:       int(pg.Version),
					Role:            "primary",
					PermTier:        "T0",
					Capabilities:    []string{}, // Will be populated by checks
					Databases: []wire.Database{
						{Name: "app", Monitored: true},
					},
					Results: []wire.Result{},
				},
			},
		}

		// Collect results from all runnable checks, but only include stable checks.
		// Some checks like stat_statements vary based on runtime queries, making
		// golden comparison impossible. activity is excluded too: its pg_backends
		// metric emits one row per distinct (state, wait_event_type) among OTHER
		// live backends, so the row count depends on concurrent connection/pool
		// state at scrape time and is not reproducible across runs. We focus on
		// the checks with deterministic output: instance_info and database_stats.
		stableChecks := map[string]bool{
			"instance_info":  true,
			"database_stats": true,
		}

		checks := check.All()
		for _, c := range checks {
			// Only include stable checks in the golden comparison
			if !stableChecks[c.Name()] {
				continue
			}

			reqs := c.Requires()
			canRun, _ := reqs.Supports(pgtype.RolePrimary, pg.Version, pgtype.TierReadOnly, extensions)
			if !canRun {
				continue
			}

			// Acquire a fresh connection for this check (checks call Release() on it)
			dataConn, err := dataPool.Acquire(ctx)
			require.NoError(t, err)

			target := &testTarget{
				conn:       dataConn,
				version:    pg.Version,
				database:   "app",
				instanceID: uuid.New(),
				clusterID:  pgtype.ClusterID(1),
				role:       pgtype.RolePrimary,
				permTier:   pgtype.TierReadOnly,
				extensions: extensions,
			}

			result, err := c.Scrape(ctx, target)
			if err != nil {
				continue // Skip checks that error — they may have optional requirements
			}

			// Convert check.Result to wire.Result
			wireResult := wire.Result{
				Check:      c.Name(),
				TS:         now,
				Database:   "",
				StatsReset: result.StatsReset,
				Truncated:  result.Truncated,
				Metrics:    make([]wire.Metric, len(result.Metrics)),
				QueryTexts: make(map[string]string), // Wire format uses string keys, not int64
			}

			// Convert query texts from int64 keys to string keys
			for queryid, text := range result.QueryTexts {
				wireResult.QueryTexts[fmt.Sprintf("%d", queryid)] = text
			}

			// Convert metrics
			for i, m := range result.Metrics {
				wireResult.Metrics[i] = wire.Metric{
					Name:   m.Name,
					Labels: m.Labels,
					Value:  m.Value,
					Kind:   string(m.Kind), // MetricKind is a string type alias
				}
			}

			env.Instances[0].Results = append(env.Instances[0].Results, wireResult)
		}

		// Normalize the envelope for golden comparison
		normalized := normalizeEnvelope(env)

		// Assert against golden file
		goldenPath := filepath.Join("testdata", fmt.Sprintf("payload_pg%d.json", pg.Major))
		wire.AssertGolden(t, normalized, goldenPath, false)

		t.Logf("envelope for pg%d has %d results", pg.Major, len(env.Instances[0].Results))
	})
}
