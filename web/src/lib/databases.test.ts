import { describe, expect, it } from 'vitest'

import { defaultDatabase, partitionDatabases } from './databases'

describe('database helpers', () => {
  it('UI-INST-010 partitions monitored databases and groups skipped reasons', () => {
    const result = partitionDatabases([
      { datname: 'app', monitored: true },
      { datname: 'analytics', monitored: false, skip_reason: 'db_budget' },
      { datname: 'audit', monitored: false, skip_reason: 'excluded' },
      { datname: 'legacy', monitored: false, skip_reason: 'db_budget' },
      { datname: 'unknown', monitored: false, skip_reason: null },
    ])

    expect(result.monitored.map((database) => database.datname)).toEqual(['app'])
    expect(result.skipped).toEqual(
      new Map([
        ['db_budget', ['analytics', 'legacy']],
        ['excluded', ['audit']],
        ['unknown', ['unknown']],
      ]),
    )
  })

  it('UI-INST-011 defaultDatabase picks the most active monitored database', () => {
    const selected = defaultDatabase([
      { datname: 'quiet', monitored: true, activity: 2 },
      { datname: 'busy', monitored: true, activity: 11 },
      { datname: 'skipped', monitored: false, activity: 100 },
    ])

    expect(selected?.datname).toBe('busy')
  })

  it('UI-INST-012 defaultDatabase returns null when none are monitored', () => {
    expect(
      defaultDatabase([{ datname: 'excluded', monitored: false, skip_reason: 'excluded' }]),
    ).toBeNull()
  })
})
