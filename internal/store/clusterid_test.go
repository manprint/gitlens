package store

import (
	"math"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestClusterID_DBRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []pgtype.ClusterID{0, 1, math.MaxInt64, math.MaxInt64 + 1, math.MaxUint64}
	for _, c := range cases {
		require.Equal(t, c, FromDB(ToDB(c)))
	}
	require.Equal(t, int64(-1), ToDB(math.MaxUint64))
}
