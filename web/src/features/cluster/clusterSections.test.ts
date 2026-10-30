import { describe, expect, it } from 'vitest'

import { CLUSTER_SECTIONS, DEFAULT_CLUSTER_SECTION, readClusterSection } from './clusterSections'

describe('cluster sections registry', () => {
  it('CLTAB-001 defaults to Topology and CLTAB-003 falls back for unknown values', () => {
    expect(DEFAULT_CLUSTER_SECTION).toBe('topology')
    expect(readClusterSection(null)).toBe('topology')
    expect(readClusterSection('lag')).toBe('lag')
    expect(readClusterSection('nonsense')).toBe('topology')
  })

  it('CLTAB-005 only the lag tab is a chart tab', () => {
    expect(CLUSTER_SECTIONS.map((section) => section.id)).toEqual([
      'topology',
      'lag',
      'slots',
      'drift',
      'events',
    ])
    expect(
      CLUSTER_SECTIONS.filter((section) => section.kind === 'charts').map((section) => section.id),
    ).toEqual(['lag'])
  })
})
