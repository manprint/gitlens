import type { Cluster } from '@/api/types'

/**
 * These explanations mirror README.md's GET /api/v1/clusters “Fields
 * explained” rules: ok means a primary exists, every instance is up, and no
 * replication lag is measurable; degraded means a primary exists but an
 * instance is down or a replica has measurable replay lag; critical means no
 * primary or more than one primary (split brain). The health field itself is
 * always supplied by the server and is never recomputed here.
 */
export function describeHealth(cluster: Cluster): string {
  const primaryCount = cluster.instances.filter((instance) => instance.role === 'primary').length

  switch (cluster.health) {
    case 'ok':
      return 'Healthy: a primary is present, all instances are up, and no replication lag is measurable.'
    case 'degraded': {
      const downInstance = cluster.instances.find((instance) => !instance.up)
      if (downInstance) return `Degraded: instance ${downInstance.addr} is down.`

      if (cluster.max_replay_lag_seconds != null && cluster.max_replay_lag_seconds > 0) {
        return `Degraded: standby replay lag is ${cluster.max_replay_lag_seconds} seconds.`
      }

      return 'Degraded: the cluster has a primary, but a health check requires attention.'
    }
    case 'critical':
      if (primaryCount > 1)
        return 'Critical: split brain detected; more than one primary is reporting.'
      return 'Critical: no primary is reporting.'
  }
}
