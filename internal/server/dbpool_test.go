package server

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestAsDBPool_Nil(t *testing.T) {
	t.Parallel()
	require.Nil(t, asDBPool(nil))
}

func TestAsDBPool_NonNil(t *testing.T) {
	t.Parallel()
	// A zero-value *pgxpool.Pool is never connected to anything and its
	// methods are never called here — asDBPool only checks the pointer
	// itself, so this is enough to exercise the non-nil branch at L1.
	require.NotNil(t, asDBPool(&pgxpool.Pool{}))
}
