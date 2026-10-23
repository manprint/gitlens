package check

import (
	"context"
	"errors"
	"math"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func int64p(v int64) *int64 { return &v }

// sequenceTestRow is one sequencesQuery row: an ascending integer sequence
// owned by an integer column unless changed.
func sequenceTestRow(name string, oid uint32, readable bool) []any {
	return []any{"public." + name, oid, "public", name, "integer",
		int64(1), int64(1), int64(1), int64(math.MaxInt32), false, readable,
		"public", "t_" + name, "id", "integer", int32(2), int64p(math.MaxInt32), int64p(math.MinInt32), int64(1)}
}

func scrapeSequencesForTest(t *testing.T, seqs [][]any, values func(batch []uint32) (pgx.Rows, error)) (Result, int, error) {
	t.Helper()
	batches := 0
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		switch sql {
		case sequencesQuery:
			require.Equal(t, []any{sequenceScanLimit + 1}, args)
			return &mockRows{rows: seqs}, nil
		case sequenceValuesQuery:
			batches++
			batch := args[0].([]uint32)
			require.LessOrEqual(t, len(batch), sequenceValueBatch)
			return values(batch)
		}
		t.Fatalf("unexpected query %q", sql)
		return nil, nil
	}}
	r, err := (&sequencesCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.Equal(t, 1, conn.releaseCalls)
	return r, batches, err
}

func TestSequences_Requirements(t *testing.T) {
	c := &sequencesCheck{}
	require.Equal(t, "sequences", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Equal(t, 10*time.Minute, c.DefaultInterval())
}

// The audit of INT-RO-001 refuses any statement naming a function that
// advances a sequence; the check's own statements must not even mention one.
func TestSequences_QueriesNeverAdvance(t *testing.T) {
	advance := regexp.MustCompile(`(?i)\b(nextval|setval)\b`)
	require.False(t, advance.MatchString(sequencesQuery))
	require.False(t, advance.MatchString(sequenceValuesQuery))
}

// A NULL value means "never called" only when the role may read the
// sequence; without the privilege the value is hidden, not zero.
func TestSequences_UnusedIsNotHidden(t *testing.T) {
	r, batches, err := scrapeSequencesForTest(t,
		[][]any{sequenceTestRow("fresh", 10, true), sequenceTestRow("secret", 11, false)},
		func(batch []uint32) (pgx.Rows, error) {
			require.Equal(t, []uint32{10}, batch)
			return &mockRows{rows: [][]any{{uint32(10), (*int64)(nil)}}}, nil
		})
	require.NoError(t, err)
	require.Equal(t, 1, batches)
	byName := map[string]Fact{}
	for _, f := range r.Facts {
		byName[f.Labels["sequence"]] = f
	}
	require.Equal(t, "unused", byName["fresh"].ValueText)
	require.Equal(t, "0.000000", byName["fresh"].Labels["used_ratio"])
	require.Equal(t, "2147483647", byName["fresh"].Labels["remaining"])
	require.Equal(t, "hidden", byName["secret"].ValueText)
	require.Empty(t, byName["secret"].Labels["used_ratio"])
	require.Empty(t, byName["secret"].Labels["remaining"])
	require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_sequences_value_hidden", Value: 1, Kind: pgtype.KindGauge})
	require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_sequence_max_used_ratio", Value: 0, Kind: pgtype.KindGauge})
}

// Values are read a batch per statement, so the locks a read takes are
// released every sequenceValueBatch sequences.
func TestSequences_ReadsInBatches(t *testing.T) {
	var seqs [][]any
	for i := range 250 {
		seqs = append(seqs, sequenceTestRow("s"+string(rune('a'+i%26))+string(rune('a'+i/26)), uint32(100+i), true))
	}
	r, batches, err := scrapeSequencesForTest(t, seqs, func(batch []uint32) (pgx.Rows, error) {
		rows := make([][]any, len(batch))
		for i, oid := range batch {
			rows[i] = []any{oid, int64p(int64(oid))}
		}
		return &mockRows{rows: rows}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, batches)
	require.Len(t, r.Facts, 250)
	require.False(t, r.Partial)
	// The highest used fraction first.
	require.Equal(t, "349", r.Facts[0].Labels["last_value"])
}

// A batch that waits too long for a lock leaves its sequences unread and the
// result partial; another error fails the check.
func TestSequences_LockTimeoutIsPartial(t *testing.T) {
	r, _, err := scrapeSequencesForTest(t, [][]any{sequenceTestRow("busy", 10, true)},
		func([]uint32) (pgx.Rows, error) {
			return nil, &pgconn.PgError{Code: pgLockNotAvailable}
		})
	require.NoError(t, err)
	require.True(t, r.Partial)
	require.Equal(t, "unread", r.Facts[0].ValueText)

	_, _, err = scrapeSequencesForTest(t, [][]any{sequenceTestRow("busy", 10, true)},
		func([]uint32) (pgx.Rows, error) {
			return nil, errors.New("connection reset")
		})
	require.ErrorContains(t, err, "connection reset")
}

func TestSequences_PartialAboveFactLimit(t *testing.T) {
	var seqs [][]any
	for i := range sequenceFactLimit + 1 {
		seqs = append(seqs, sequenceTestRow("s", uint32(i), false))
	}
	r, _, err := scrapeSequencesForTest(t, seqs, nil)
	require.NoError(t, err)
	require.True(t, r.Partial)
	require.False(t, r.Truncated)
	require.Len(t, r.Facts, sequenceFactLimit)
	require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_sequences", Value: sequenceFactLimit + 1, Kind: pgtype.KindGauge})
}

func TestSequenceUsage(t *testing.T) {
	ratio := func(u sequenceUsage) float64 { require.NotNil(t, u.ratio); return *u.ratio }
	for name, tc := range map[string]struct {
		row         sequenceRow
		limit       int64
		columnBinds bool
		exhausts    bool
		ratio       float64
		remaining   string
	}{
		"bigint sequence on an integer column": {
			row:   sequenceRow{start: 1, increment: 1, min: 1, max: math.MaxInt64, typeMax: int64p(math.MaxInt32), typeMin: int64p(math.MinInt32), read: true, lastValue: int64p(math.MaxInt32 - 10)},
			limit: math.MaxInt32, columnBinds: true, exhausts: true, ratio: 1 - 10.0/(math.MaxInt32-1), remaining: "10",
		},
		"full-range descending bigint": {
			row:   sequenceRow{start: -1, increment: -1, min: math.MinInt64, max: -1, read: true, lastValue: int64p(math.MinInt64 / 2)},
			limit: math.MinInt64, exhausts: true, ratio: 0.5, remaining: "4611686018427387904",
		},
		"increment larger than one": {
			row:   sequenceRow{start: 0, increment: 10, min: 0, max: 1000, read: true, lastValue: int64p(995)},
			limit: 1000, exhausts: true, ratio: 0.995, remaining: "0",
		},
		"past a narrower column": {
			row:   sequenceRow{start: 1, increment: 1, min: 1, max: math.MaxInt64, typeMax: int64p(math.MaxInt16), read: true, lastValue: int64p(40000)},
			limit: math.MaxInt16, columnBinds: true, exhausts: true, ratio: 1, remaining: "0",
		},
		"cycling within its own bound": {
			row:   sequenceRow{start: 1, increment: 1, min: 1, max: 100, cycle: true, typeMax: int64p(math.MaxInt64), read: true, lastValue: int64p(51)},
			limit: 100, exhausts: false, ratio: 50.0 / 99, remaining: "49",
		},
		"cycling past its column": {
			row:   sequenceRow{start: 1, increment: 1, min: 1, max: math.MaxInt64, cycle: true, typeMax: int64p(math.MaxInt32), read: true, lastValue: int64p(1)},
			limit: math.MaxInt32, columnBinds: true, exhausts: true, ratio: 0, remaining: "2147483646",
		},
		"never called": {
			row:   sequenceRow{start: 5, increment: 5, min: 1, max: 100, read: true},
			limit: 100, exhausts: true, ratio: 0, remaining: "20",
		},
	} {
		t.Run(name, func(t *testing.T) {
			tc.row.readable = true
			u := sequenceUsageOf(&tc.row)
			require.Equal(t, tc.limit, u.limit)
			require.Equal(t, tc.columnBinds, u.columnBinds)
			require.Equal(t, tc.exhausts, u.exhausts)
			require.InDelta(t, tc.ratio, ratio(u), 1e-9)
			require.Equal(t, tc.remaining, u.remaining.String())
		})
	}
}
