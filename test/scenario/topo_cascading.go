//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-REPL-006",
		Title:    "cascading standby keeps its upstream after the parent stops",
		Topology: Topology("cascading"),
		Covers:   []string{"phase_10.md#9.1", "I-1"},
		Expect:   Expectations{Invariants: []string{"I-1"}},
		Run:      runCascadingReplicationTopology,
	})
}

func runCascadingReplicationTopology(ctx context.Context, e *Env) error {
	// The API contract in this checkout exposes topology below a cluster. The
	// cluster list is used first to discover the stable cluster id, then the
	// topology endpoint is polled until all three monitored instances and both
	// replication edges are visible.
	var cluster map[string]interface{}
	var clusterID string
	var initialTopology map[string]interface{}
	resolveCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if err := poll(resolveCtx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		clusters, err := e.API.Clusters()
		if err != nil {
			return false, err
		}
		if len(clusters) != 1 {
			return false, fmt.Errorf("want one cluster, got %d", len(clusters))
		}
		cluster = clusters[0]
		var ok bool
		clusterID, ok = cluster["cluster_id"].(string)
		if !ok || clusterID == "" {
			return false, fmt.Errorf("cluster response has no cluster_id: %v", cluster)
		}
		initialTopology, err = e.API.Get("/api/v1/clusters/" + clusterID + "/topology")
		if err != nil {
			return false, err
		}
		return topologyHasThreeNodesAndTwoEdges(cluster, initialTopology), nil
	}); err != nil {
		return fmt.Errorf("cascading topology did not converge: %w; cluster=%v topology=%v", err, cluster, initialTopology)
	}
	if clusterID == "" || !topologyHasThreeNodesAndTwoEdges(cluster, initialTopology) {
		return fmt.Errorf("cascading topology did not converge: cluster=%v topology=%v", cluster, initialTopology)
	}

	instances, ok := cluster["instances"].([]interface{})
	if !ok {
		return fmt.Errorf("clusters response missing instances: %v", cluster)
	}
	ids := make(map[string]string, len(instances))
	roles := make(map[string]string, len(instances))
	for addr, wantRole := range map[string]string{
		"pg-primary":   "primary",
		"pg-standby-a": "standby",
		"pg-standby-b": "standby",
	} {
		pgConfig := e.PG(addr).Config().ConnConfig
		for _, raw := range instances {
			instance, ok := raw.(map[string]interface{})
			if !ok {
				return fmt.Errorf("invalid instance entry: %v", raw)
			}
			instanceAddr, _ := instance["addr"].(string)
			instancePort, _ := instance["port"].(float64)
			id, _ := instance["instance_id"].(string)
			role, _ := instance["role"].(string)
			if instanceAddr != addr && (instanceAddr != pgConfig.Host || instancePort != float64(pgConfig.Port)) {
				continue
			}
			ids[addr], roles[addr] = id, role
			break
		}
		if roles[addr] != wantRole || ids[addr] == "" {
			return fmt.Errorf("unexpected role or missing instance for %s: roles=%v ids=%v", addr, roles, ids)
		}
	}

	if err := e.Compose("stop", "pg-standby-a"); err != nil {
		return fmt.Errorf("stop cascading parent: %w", err)
	}
	e.T.Cleanup(func() { _ = e.Compose("start", "pg-standby-a") })

	var stale map[string]interface{}
	staleCtx, cancel2 := context.WithTimeout(ctx, 120*time.Second)
	defer cancel2()
	if err := poll(staleCtx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		clusters, err := e.API.Clusters()
		if err != nil {
			return false, err
		}
		if len(clusters) != 1 {
			return false, fmt.Errorf("cluster disappeared after parent stop")
		}
		stale = clusters[0]
		topo, err := e.API.Get("/api/v1/clusters/" + clusterID + "/topology")
		if err != nil {
			return false, err
		}
		return parentIsDownAndBStillPointsToA(stale, topo, ids["pg-standby-a"], ids["pg-standby-b"]), nil
	}); err != nil {
		return fmt.Errorf("cascading edge was not marked stale without reparenting: %w; state=%v", err, stale)
	}
	e.AssertInvariants(e.T)
	return nil
}

func topologyHasThreeNodesAndTwoEdges(cluster, topology map[string]interface{}) bool {
	instances, ok := cluster["instances"].([]interface{})
	edges, okEdges := topology["topology"].([]interface{})
	return ok && okEdges && len(instances) == 3 && len(edges) == 2
}

func parentIsDownAndBStillPointsToA(cluster, topology map[string]interface{}, parentID, childID string) bool {
	instances, ok := cluster["instances"].([]interface{})
	if !ok {
		return false
	}
	parentDown := false
	for _, raw := range instances {
		instance, ok := raw.(map[string]interface{})
		if !ok || instance["instance_id"] != parentID {
			continue
		}
		up, _ := instance["up"].(bool)
		parentDown = !up
	}
	if !parentDown {
		return false
	}
	edges, ok := topology["topology"].([]interface{})
	if !ok || len(edges) != 2 {
		return false
	}
	for _, raw := range edges {
		edge, ok := raw.(map[string]interface{})
		if ok && edge["from"] == childID && edge["to"] == parentID {
			return true
		}
	}
	return false
}
