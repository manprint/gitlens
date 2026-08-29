package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrations_0006_Parses(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0006_alerts.sql")
	require.NoError(t, err)
	require.NotEmpty(t, data)

	sql := string(data)
	for _, table := range []string{"alert_rules", "alerts", "silences", "notifications"} {
		require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS "+table)
	}
}
