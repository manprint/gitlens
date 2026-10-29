import { describe, expect, it } from 'vitest'

import { ASH_WAIT_TYPES } from '@/components/charts/ash.options'

import {
  ASH_WAIT_DESCRIPTIONS,
  groupOfType,
  groupSeries,
  seriesAverage,
  waitDescription,
} from './ashGroups'

const series = (name: string, ...values: (number | null)[]) => ({
  name,
  points: values.map((value, index) => ({ ts: `2026-10-29T10:0${index}:00Z`, value })),
})

const ids = (groups: ReturnType<typeof groupSeries>) => groups.map(({ group }) => group.id)
const names = (groups: ReturnType<typeof groupSeries>, id: string) =>
  groups.find(({ group }) => group.id === id)?.series.map(({ name }) => name)

describe('ashGroups', () => {
  it('ASHGRP-001 puts every wait event type in its group', () => {
    const all = [
      'CPU',
      'IO',
      'Lock',
      'LWLock',
      'BufferPin',
      'Buffer',
      'Client',
      'IPC',
      'Timeout',
      'Activity',
      'Extension',
      'other',
    ].map((name) => series(name, 1))
    const groups = groupSeries(all, 'wait_event_type')

    expect(names(groups, 'cpu')).toEqual(['CPU'])
    expect(names(groups, 'io')).toEqual(['IO'])
    expect(names(groups, 'locks')).toEqual(['Lock', 'LWLock', 'BufferPin', 'Buffer'])
    expect(names(groups, 'client')).toEqual(['Client', 'IPC'])
    expect(names(groups, 'other')).toEqual(['Timeout', 'Activity', 'Extension', 'other'])
  })

  it('ASHGRP-002 leaves out empty groups and keeps the fixed order', () => {
    const groups = groupSeries(
      [series('Timeout', 1), series('Client', 1), series('CPU', 1)],
      'wait_event_type',
    )

    expect(ids(groups)).toEqual(['cpu', 'client', 'other'])
    expect(groupSeries([], 'wait_event_type')).toEqual([])
  })

  it('ASHGRP-003 averages over sampled buckets and shares add up to one', () => {
    const groups = groupSeries(
      [series('CPU', 1, 3, null), series('Lock', 0.5, 0.5), series('LWLock', 1, null)],
      'wait_event_type',
    )
    const cpu = groups.find(({ group }) => group.id === 'cpu')
    const locks = groups.find(({ group }) => group.id === 'locks')

    expect(cpu?.total).toBe(2)
    expect(locks?.total).toBe(1.5)
    expect(cpu?.share).toBeCloseTo(2 / 3.5, 12)
    expect(groups.reduce((sum, { share }) => sum + share, 0)).toBeCloseTo(1, 9)

    const idle = groupSeries([series('CPU', 0, 0), series('IO', null)], 'wait_event_type')
    expect(idle.map(({ share }) => share)).toEqual([0, 0])
    expect(idle.every(({ total, share }) => Number.isFinite(total) && !Number.isNaN(share))).toBe(
      true,
    )
    expect(seriesAverage(series('CPU', null, null))).toBe(0)
  })

  it('ASHGRP-004 puts an unknown wait type in other, never dropping it', () => {
    const groups = groupSeries([series('NewTypeInPG20', 1)], 'wait_event_type')

    expect(ids(groups)).toEqual(['other'])
    expect(names(groups, 'other')).toEqual(['NewTypeInPG20'])
    expect(groupOfType(undefined)).toBe('other')
    expect(waitDescription('NewTypeInPG20')).toBe('Wait event type NewTypeInPG20')
  })

  it('ASHGRP-005 groups wait events by the type the server carried', () => {
    const types = new Map([
      ['WALWrite', 'LWLock'],
      ['ClientRead', 'Client'],
      ['DataFileRead', 'IO'],
      ['CPU', 'CPU'],
    ])
    const groups = groupSeries(
      [
        series('WALWrite', 1),
        series('ClientRead', 1),
        series('DataFileRead', 1),
        series('CPU', 1),
        series('Unmapped', 1),
        series('other (folded)', 1),
      ],
      'wait_event',
      types,
    )

    expect(ids(groups)).toEqual(['cpu', 'io', 'locks', 'client', 'other'])
    expect(names(groups, 'locks')).toEqual(['WALWrite'])
    expect(names(groups, 'other')).toEqual(['Unmapped', 'other (folded)'])
  })

  it('ASHGRP-006 describes every charted wait event type', () => {
    for (const type of ASH_WAIT_TYPES) {
      expect(ASH_WAIT_DESCRIPTIONS[type], type).toMatch(/\S/u)
      expect(waitDescription(type)).not.toMatch(/^Wait event type /u)
    }
  })
})
