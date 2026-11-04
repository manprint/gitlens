import { INSTANCE_ID, NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  instance_id: INSTANCE_ID,
  sampled_at: NOW,
  stale: false,
  nodes: [],
  sessions: [],
}
export const empty = base
export const stale = { ...base, stale: true }
export const neverSampled = { ...base, sampled_at: null }
const origins = [
  { key: 'application', count: 2 },
  { key: 'pglens', count: 1 },
]
export const withOrigins = {
  ...base,
  sessions: [
    {
      pid: 4101,
      usename: 'app',
      datname: 'shop',
      application_name: 'billing',
      client_addr: '10.0.0.5',
      state: 'active',
      xact_age: 12.5,
      state_age: 1.5,
      query_age: 1.5,
      query_id: '-4611686018427387904',
      backend_xid_age: 3,
      backend_xmin_age: 5,
      blocked_by: [],
      origin: 'application',
      query_text: 'SELECT * FROM orders WHERE id = $1',
      query_text_source: 'normalised',
    },
    {
      pid: 4102,
      usename: 'app',
      datname: 'shop',
      application_name: 'billing',
      client_addr: '10.0.0.5',
      state: 'idle in transaction',
      xact_age: 40,
      state_age: 40,
      blocked_by: [],
      origin: 'application',
      query_text: 'SELECT 1 /* raw */',
      query_text_source: 'raw',
    },
    {
      pid: 4103,
      usename: 'pglens_monitor',
      datname: 'shop',
      application_name: 'pglens/check',
      client_addr: '',
      state: 'idle',
      xact_age: 0,
      state_age: 4,
      blocked_by: [],
      origin: 'pglens',
    },
  ],
  connections: {
    by_application_enabled: true,
    truncated: false,
    by_origin: origins,
    by_application: [
      { key: 'billing', count: 2 },
      { key: 'pglens/check', count: 1 },
    ],
    by_user: [
      { key: 'app', count: 2 },
      { key: 'pglens_monitor', count: 1 },
    ],
    by_client: [
      { key: '10.0.0.5', count: 2 },
      { key: 'local', count: 1 },
    ],
    by_database: [{ key: 'shop', count: 3 }],
    by_state: [
      { key: 'active', count: 1 },
      { key: 'idle', count: 1 },
      { key: 'idle in transaction', count: 1 },
    ],
  },
}
export const withTruncatedConnections = {
  ...withOrigins,
  connections: {
    ...withOrigins.connections,
    by_application_enabled: false,
    truncated: true,
    totals: {
      by_origin: [
        { key: 'application', count: 240 },
        { key: 'pglens', count: 4 },
      ],
      by_application: [],
      by_user: [{ key: 'app', count: 244 }],
      by_client: [{ key: 'local', count: 244 }],
      by_database: [{ key: 'shop', count: 244 }],
      by_state: [
        { key: 'idle', count: 239 },
        { key: 'active', count: 5 },
      ],
    },
  },
}
export function makeGetLocks(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
