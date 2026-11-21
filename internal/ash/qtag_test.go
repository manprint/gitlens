package ash

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
)

// tailRows answers the tagged sample query: the eight phase 2 columns and the
// statement tail.
type tailRows struct {
	mockRows
	tails []*string
}

func (m *tailRows) Scan(dest ...any) error {
	i := m.idx
	if err := m.mockRows.Scan(dest[:8]...); err != nil {
		return err
	}
	if len(dest) > 8 {
		*dest[8].(**string) = m.tails[i]
	}
	return nil
}

type tailQuerier struct {
	rows    pgx.Rows
	lastSQL string
}

func (q *tailQuerier) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	q.lastSQL = sql
	return q.rows, nil
}

func strPtr(s string) *string { return &s }

// TestQTAG006: a tagged statement tail becomes Service and Endpoint; the tail
// itself is in no field of the row.
func TestQTAG006(t *testing.T) {
	tail := `SELECT pg_sleep(3) /*application='shop',controller='cart',action='show',traceparent='00-5bd66ef5095369c7b0d1f8f4bd33716a-c532cb4098ac3dd2-01'*/`
	rows := &tailRows{
		mockRows: mockRows{rows: [][5]any{
			{"app", "active", "CPU", "CPU", (*int64)(nil)},
			{"app", "active", "CPU", "CPU", (*int64)(nil)},
		}},
		tails: []*string{strPtr(tail), nil},
	}
	q := &tailQuerier{rows: rows}
	s := NewSampler(q, clock.NewFake(time.Unix(0, 0)), nil)
	s.SetQueryTags(true)
	batch, err := s.sampleBatch(context.Background())
	require.NoError(t, err)
	require.Contains(t, q.lastSQL, "right(query, 700)                 AS query_tail")
	require.Len(t, batch, 2)
	require.Equal(t, "shop", batch[0].Service)
	require.Equal(t, "cart#show", batch[0].Endpoint)
	require.Empty(t, batch[1].Service+batch[1].Endpoint)

	v := reflect.ValueOf(batch[0])
	for i := 0; i < v.NumField(); i++ {
		if f, ok := v.Field(i).Interface().(string); ok {
			require.NotContains(t, f, "pg_sleep", v.Type().Field(i).Name)
			require.NotContains(t, f, "traceparent", v.Type().Field(i).Name)
		}
	}
}

// TestQTAG007: with 150 distinct endpoints the cap of 100 keys holds and the
// overflow bucket conserves the total (I-2).
func TestQTAG007(t *testing.T) {
	a := New(clock.NewFake(time.Unix(0, 0)), 10*time.Second, 100)
	total := 0
	for i := 0; i < 150; i++ {
		for j := 0; j <= i%3; j++ {
			a.Add(SampleRow{Datname: "app", State: "active", WaitEventType: "CPU", WaitEvent: "CPU",
				Service: "shop", Endpoint: fmt.Sprintf("/e%03d", i)})
			total++
		}
	}
	w := a.Flush(time.Unix(10, 0))
	require.True(t, w.Truncated)
	require.Len(t, w.Buckets, 100)
	sum := 0
	for _, b := range w.Buckets {
		sum += b.Samples
		if b.Key != otherKey {
			require.Equal(t, "shop", b.Key.Service)
		}
	}
	require.Equal(t, total, sum)
	require.Empty(t, otherKey.Service+otherKey.Endpoint)
}

// TestQTAG008: without the tag dimensions the query is the phase 2 text, byte
// for byte, and reads no statement tail.
func TestQTAG008(t *testing.T) {
	const phase2 = `
SELECT COALESCE(datname, '')             AS datname,
       state                             AS state,
       COALESCE(wait_event_type, 'CPU')  AS wait_event_type,
       COALESCE(wait_event, 'CPU')       AS wait_event,
       query_id,
       COALESCE(usename, '')             AS usename,
       COALESCE(host(client_addr), '')   AS client_addr,
       COALESCE(application_name, '')    AS application_name
  FROM pg_stat_activity
 WHERE backend_type IN ('client backend', 'parallel worker')
   AND state <> 'idle'
   AND pid <> pg_backend_pid()
   AND COALESCE(application_name, '') NOT LIKE 'pglens/%'
	`
	require.Equal(t, phase2, sampleQuery(false))
	require.NotContains(t, sampleQuery(false), "query_tail")

	q := &mockQuerier{rows: &mockRows{}}
	s := NewSampler(q, clock.NewFake(time.Unix(0, 0)), nil)
	_, err := s.sampleBatch(context.Background())
	require.NoError(t, err)
	require.Equal(t, phase2, q.lastSQL)
	require.True(t, strings.HasSuffix(sampleQuery(true), sampleFrom))
}
