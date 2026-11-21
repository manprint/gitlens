import type { ClusterEvent } from '@/lib/events'

/** A watcher capture event and a deadlock payload for incident tests. */
export function incidentEvent(
  eventId: number,
  payload: Record<string, unknown>,
  type = 'deadlock_captured',
): ClusterEvent {
  return {
    event_id: eventId,
    ts: '2026-11-20T16:02:05Z',
    type,
    cluster_id: '1',
    instance_id: '22222222-2222-2222-2222-222222222222',
    payload,
  }
}

export const deadlockPayload = {
  id: 'deadlock-1',
  type: 'deadlock',
  captured_at: '2026-11-20T16:02:03Z',
  database: 'app',
  relation: 'public.orders',
  ctid: '(3,7)',
  lock_mode: 'ShareLock',
  wait_seconds: 1.2,
  sessions: [
    {
      pid: 101,
      usename: 'app_rw',
      application_name: 'web',
      client_addr: '10.1.1.5',
      state: 'active',
      xact_seconds: 4,
      query_seconds: 1.5,
      query: 'UPDATE orders SET qty = 1 WHERE id = 7',
      blocked_by: [102],
      wait: {
        locktype: 'transactionid',
        mode: 'ShareLock',
        transactionid: '900',
        wait_seconds: 1.2,
      },
      row: {
        locktype: 'tuple',
        mode: 'ExclusiveLock',
        relation: 'public.orders',
        page: 3,
        tuple: 7,
      },
      held_relations: ['public.orders', 'public.order_lines'],
      xmin_age: 1200,
    },
    {
      pid: 102,
      state: 'idle in transaction',
      state_seconds: 30,
      query: 'UPDATE orders SET qty = 2 WHERE id = 7',
      blocked_by: [101],
      wait: {
        locktype: 'tuple',
        mode: 'ExclusiveLock',
        relation: 'public.orders',
        page: 3,
        tuple: 7,
      },
    },
    { bad: true },
  ],
  sessions_omitted: 2,
  suppressed_before: 3,
}
