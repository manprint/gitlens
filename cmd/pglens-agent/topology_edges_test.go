package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/wire"
)

// streamingReceiver is one replication_receiver result for a walreceiver that
// is streaming from sender:5432.
func streamingReceiver(sender string) []wire.Result {
	return []wire.Result{{
		Check: "replication_receiver",
		Metrics: []wire.Metric{
			{Name: "replication_receiver_status", Value: 1, Labels: map[string]string{"sender_host": sender}},
			{Name: "replication_receiver_sender_port", Value: 5432},
		},
	}}
}

// TestEdgeStatesBuild_ConcurrentFlushesAreSerialised: the push ticker and the
// shutdown flush both call build, and they can overlap. Every goroutine writes
// the shared map here. Without the lock this is a data race, which -race
// reports and the runtime turns into a fatal "concurrent map writes".
func TestEdgeStatesBuild_ConcurrentFlushesAreSerialised(t *testing.T) {
	states := &edgeStates{byName: map[string]edgeState{}}
	reading := streamingReceiver("10.0.0.1")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("pg-%d", i)
			for range 300 {
				states.build(name, "instance-"+name, reading)
			}
		}()
	}
	wg.Wait()

	for i := range 8 {
		name := fmt.Sprintf("pg-%d", i)
		edges := states.build(name, "instance-"+name, reading)
		require.Equal(t, []wire.Edge{{From: "instance-" + name, To: "10.0.0.1", Port: 5432, Type: "streaming", Confidence: "high"}}, edges)
	}
}
