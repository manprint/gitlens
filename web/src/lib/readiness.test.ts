import { describe, expect, it } from 'vitest'

import {
  clusterReadiness,
  groupByChannel,
  readinessBadgeLabel,
  readinessBadgeText,
  readinessLevel,
  severityRank,
  type ReadinessFix,
} from './readiness'

const fix = (channel: ReadinessFix['channel'], title: string): ReadinessFix => ({
  channel,
  title,
  steps: [],
  requires_restart: false,
})

describe('readiness helpers', () => {
  it('RDYLIB-001 the level is partial only with a warning; warnings rank first', () => {
    expect(readinessLevel({ counts: { warning: 0, info: 5 } })).toBe('full')
    expect(readinessLevel({ counts: { warning: 1, info: 0 } })).toBe('partial')
    expect(severityRank('warning')).toBeLessThan(severityRank('info'))
  })

  it('RDYLIB-002 fixes group by channel in a fixed order, empty channels left out', () => {
    const groups = groupByChannel([fix('sql', 'a'), fix('console', 'b'), fix('sql', 'c')])
    expect(groups.map((group) => group.channel)).toEqual(['console', 'sql'])
    expect(groups[1]!.fixes.map((entry) => entry.title)).toEqual(['a', 'c'])
    expect(groupByChannel([])).toEqual([])
  })

  it('RDYLIB-003 badge text and accessible label', () => {
    expect(readinessBadgeText('full', 0)).toBe('Full')
    expect(readinessBadgeText('partial', 1)).toBe('Partial · 1 warning')
    expect(readinessBadgeText('partial', 3)).toBe('Partial · 3 warnings')
    expect(readinessBadgeLabel('full', 0, 0)).toBe('Monitoring: full')
    expect(readinessBadgeLabel('partial', 2, 1)).toBe('Monitoring: partial, 2 warnings, 1 note')
  })

  it('RDYLIB-004 a cluster is partial when any instance is; no row means no answer', () => {
    const rows = [
      { instance_id: 'a', level: 'full' as const, warnings: 0, infos: 1 },
      { instance_id: 'b', level: 'partial' as const, warnings: 2, infos: 0 },
    ]
    expect(clusterReadiness(['a', 'b'], rows)).toEqual({ level: 'partial', warnings: 2, infos: 1 })
    expect(clusterReadiness(['a'], rows)).toEqual({ level: 'full', warnings: 0, infos: 1 })
    expect(clusterReadiness(['z'], rows)).toBeNull()
    expect(clusterReadiness(['a'], undefined)).toBeNull()
  })
})
