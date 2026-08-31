import { describe, expect, it } from 'vitest'

import { cn } from '@/lib/utils'

describe('cn', () => {
  it('merges conditional and conflicting Tailwind classes', () => {
    expect(cn('text-sm', 'px-2', undefined, 'text-lg', 'px-4')).toBe('text-lg px-4')
  })
})
