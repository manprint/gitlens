import type { Schemas } from '@/api/types'

import { INSTANCE_ID } from '../fixture-helpers'

type ReadinessReport = Schemas['ReadinessReport']

export const responseStatus = 200

const generated_at = '2026-11-04T09:00:00Z'

/** Monitored in full: no item. */
export const base: ReadinessReport = {
  instance_id: INSTANCE_ID,
  level: 'full',
  generated_at,
  counts: { warning: 0, info: 0 },
  items: [],
}
export const full = base

/** One warning with an SQL fix and one info (off by design). */
export const partial: ReadinessReport = {
  instance_id: INSTANCE_ID,
  level: 'partial',
  generated_at,
  counts: { warning: 1, info: 1 },
  items: [
    {
      id: 'RDY-SET-02',
      severity: 'warning',
      title: 'track_io_timing is off',
      summary: 'PostgreSQL does not time block reads and writes.',
      impact: ['I/O time charts', 'Per-statement I/O time'],
      evidence: 'track_io_timing = off',
      fixes: [
        {
          channel: 'sql',
          title: 'Turn it on without a restart',
          steps: ['Run the statements below as a superuser.', 'No restart is needed.'],
          snippet: 'ALTER SYSTEM SET track_io_timing = on;\nSELECT pg_reload_conf();',
          requires_restart: false,
        },
      ],
    },
    {
      id: 'RDY-SIG-01',
      severity: 'info',
      title: 'Cancel and terminate are off (by design)',
      summary: 'allow_signal is off by default.',
      impact: ['Cancel and terminate buttons'],
      evidence: 'allow_signal is false for this target',
      fixes: [
        {
          channel: 'agent_config',
          title: 'Allow signals for the target',
          steps: ['Set the key below on the target in agent.yaml.', 'Restart the agent.'],
          snippet: 'targets:\n  - name: <target>\n    allow_signal: true',
          requires_restart: true,
        },
      ],
    },
  ],
}
