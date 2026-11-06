package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// HA-V-3-4: an edge is live only while its reporting standby is up (isUp, upThreshold).
func TestHAV34LiveEdge(t *testing.T) {
	now := time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC)
	fresh := now.Add(-(upThreshold - time.Second))
	stale := now.Add(-(upThreshold + time.Second))
	silent := "The standby has not reported for more than 60 s; this is its last known upstream"

	conf, note := liveEdge("high", &fresh, now)
	require.Equal(t, "high", conf)
	require.Nil(t, note)

	conf, note = liveEdge("high", &stale, now)
	require.Equal(t, "low", conf)
	require.NotNil(t, note)
	require.Equal(t, silent, *note)

	conf, note = liveEdge("high", nil, now)
	require.Equal(t, "low", conf)
	require.Equal(t, silent, *note)

	conf, note = liveEdge("low", &fresh, now)
	require.Equal(t, "low", conf)
	require.Equal(t, lowStreamingNote, *note)

	conf, note = liveEdge("low", &stale, now)
	require.Equal(t, "low", conf)
	require.Equal(t, silent, *note)
}
