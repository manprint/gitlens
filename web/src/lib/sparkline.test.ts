import { describe, expect, it } from 'vitest'

import { sparklinePoints } from './sparkline'

const ys = (points: string) => points.split(' ').map((pair) => Number(pair.split(',')[1]))

describe('sparklinePoints', () => {
  it('SPARK-001 gives one point per value, spread from edge to edge', () => {
    const points = sparklinePoints([1, 5, 3], 100, 24).split(' ')
    expect(points).toHaveLength(3)
    expect(points[0]?.startsWith('2.0,')).toBe(true)
    expect(points[2]?.startsWith('98.0,')).toBe(true)
  })

  it('SPARK-002 puts the highest value at the top and the lowest at the bottom', () => {
    const [low, high] = ys(sparklinePoints([0, 10], 96, 24))
    expect(low).toBe(22)
    expect(high).toBe(2)
  })

  it('SPARK-003 draws a flat series on the middle line', () => {
    expect(new Set(ys(sparklinePoints([7, 7, 7], 96, 24)))).toEqual(new Set([12]))
  })
})
