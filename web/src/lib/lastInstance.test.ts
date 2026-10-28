import { describe, expect, it } from 'vitest'

import type { Cluster } from '@/api/types'
import { CLUSTER, INSTANCE_SUMMARY } from '@/test/fixture-helpers'

import { chooseInstance } from './lastInstance'

const DOWN = { ...INSTANCE_SUMMARY, instance_id: 'down-1', up: false }
const UP = { ...INSTANCE_SUMMARY, instance_id: 'up-1', up: true }
const clusters = [{ ...CLUSTER, instances: [DOWN, UP] }] as unknown as Cluster[]

describe('chooseInstance', () => {
  it('prefers a requested instance that exists', () => {
    expect(chooseInstance(clusters, 'down-1', 'up-1')).toBe('down-1')
  })

  it('falls back to the remembered instance, then the first one up', () => {
    expect(chooseInstance(clusters, 'missing', 'down-1')).toBe('down-1')
    expect(chooseInstance(clusters, null, 'missing')).toBe('up-1')
  })

  it('returns any instance when none is up, and null for an empty fleet', () => {
    const allDown = [{ ...CLUSTER, instances: [DOWN] }] as unknown as Cluster[]
    expect(chooseInstance(allDown, null, null)).toBe('down-1')
    expect(chooseInstance([], null, null)).toBeNull()
  })
})
