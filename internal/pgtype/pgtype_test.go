package pgtype_test

import (
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestClusterID_StringRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []pgtype.ClusterID{
		0,
		1,
		math.MaxUint64,
		7381927364512345678,
	}
	for _, c := range cases {
		got, err := pgtype.ParseClusterID(c.String())
		require.NoError(t, err)
		require.Equal(t, c, got)
	}
	require.Equal(t, "18446744073709551615", pgtype.ClusterID(math.MaxUint64).String())
}

func TestManualClusterID_Stable(t *testing.T) {
	t.Parallel()
	a := pgtype.ManualClusterID("my-cluster")
	b := pgtype.ManualClusterID("my-cluster")
	require.Equal(t, a, b)
	c := pgtype.ManualClusterID("other-cluster")
	require.NotEqual(t, a, c)
}

func TestCanonicalLabels(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", pgtype.CanonicalLabels(nil))
	require.Equal(t, "", pgtype.CanonicalLabels(map[string]string{}))
	require.Equal(t, "a=1", pgtype.CanonicalLabels(map[string]string{"a": "1"}))
	require.Equal(t, "a=1\x1fb=2", pgtype.CanonicalLabels(map[string]string{"b": "2", "a": "1"}))
	require.Equal(t, "a=1\x1fb=2", pgtype.CanonicalLabels(map[string]string{"a": "1", "b": "2"}))
}

func TestSeriesKey_Comparable(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	k1 := pgtype.SeriesKey{
		Metric:   "pg_backends",
		Instance: id,
		Database: "app",
		Labels:   pgtype.CanonicalLabels(map[string]string{"a": "1", "b": "2"}),
	}
	k2 := pgtype.SeriesKey{
		Metric:   "pg_backends",
		Instance: id,
		Database: "app",
		Labels:   pgtype.CanonicalLabels(map[string]string{"b": "2", "a": "1"}),
	}
	require.Equal(t, k1, k2)
	m := map[pgtype.SeriesKey]int{}
	m[k1] = 1
	require.Equal(t, 1, m[k2])
}

func TestPGVersion(t *testing.T) {
	t.Parallel()
	v := pgtype.PGVersion(160004)
	require.Equal(t, 16, v.Major())
	require.Equal(t, 4, v.Minor())
	require.Equal(t, "16.4", v.String())
	require.True(t, pgtype.PGVersion(150000).Supported())
	require.True(t, pgtype.PGVersion(160004).Supported())
	require.True(t, pgtype.PGVersion(180000).Supported())
	require.False(t, pgtype.PGVersion(140018).Supported())
	require.False(t, pgtype.PGVersion(190000).Supported())
	require.False(t, pgtype.PGVersion(0).Supported())
}

func TestRoleFromRecovery(t *testing.T) {
	t.Parallel()
	require.Equal(t, pgtype.RoleStandby, pgtype.RoleFromRecovery(true))
	require.Equal(t, pgtype.RolePrimary, pgtype.RoleFromRecovery(false))
}

func TestPermTier_StringRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tier := range []pgtype.PermTier{pgtype.TierReadOnly, pgtype.TierExplain, pgtype.TierSignal, pgtype.TierExtension} {
		s := tier.String()
		got, err := pgtype.ParsePermTier(s)
		require.NoError(t, err)
		require.Equal(t, tier, got)
	}
	_, err := pgtype.ParsePermTier("T99")
	require.Error(t, err)
	require.Equal(t, "Tier99", pgtype.PermTier(99).String())
}

func TestParseClusterID_Invalid(t *testing.T) {
	_, err := pgtype.ParseClusterID("not-a-cluster-id")
	require.Error(t, err)
}

func FuzzCanonicalLabels(f *testing.F) {
	f.Add("a", "1", "b", "2")
	f.Fuzz(func(t *testing.T, k1, v1, k2, v2 string) {
		m := map[string]string{k1: v1, k2: v2}
		// Normalize duplicate keys: if k1==k2, map has one entry
		out1 := pgtype.CanonicalLabels(m)
		out2 := pgtype.CanonicalLabels(m)
		require.Equal(t, out1, out2)
		// Check separator count
		if len(m) == 0 {
			require.Equal(t, "", out1)
		} else if len(m) == 1 {
			// single key may be empty string key? Count separators =0
			require.NotContains(t, out1, "\x1f")
		} else {
			// For distinct keys
			if k1 != k2 {
				require.Equal(t, 1, countSep(out1))
			}
		}
	})
}

func countSep(s string) int {
	n := 0
	for _, c := range s {
		if c == '\x1f' {
			n++
		}
	}
	return n
}
