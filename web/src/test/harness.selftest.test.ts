import { describe, expect, it } from 'vitest'

describe('test harness', () => {
  it('provides jsdom and jest-dom matchers', () => {
    expect(1).toBe(1)
    expect(document.body).toBeInTheDocument()
  })
})
