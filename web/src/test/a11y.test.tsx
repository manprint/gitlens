import { render } from '@testing-library/react'
import type axe from 'axe-core'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations, filterA11yViolations } from './a11y'

describe('expectNoA11yViolations', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  it('passes for a labelled button', async () => {
    const { container } = render(<button type="button">Save</button>)

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('fails for an image without alt text and identifies the element', async () => {
    const { container } = render(<img src="/logo.png" />)

    await expect(expectNoA11yViolations(container)).rejects.toThrow(/image-alt[\s\S]*element: <img/)
  })

  it('ignores minor and moderate impacts', () => {
    const fakeResults: axe.AxeResults = {
      violations: [
        {
          id: 'minor-rule',
          impact: 'minor',
          help: 'Minor issue',
          helpUrl: 'https://example.test/minor-rule',
          description: 'minor',
          tags: [],
          nodes: [],
        },
        {
          id: 'moderate-rule',
          impact: 'moderate',
          help: 'Moderate issue',
          helpUrl: 'https://example.test/moderate-rule',
          description: 'moderate',
          tags: [],
          nodes: [],
        },
      ],
      passes: [],
      incomplete: [],
      inapplicable: [],
      toolOptions: {},
      testEngine: { name: 'fake', version: '1' },
      testRunner: { name: 'fake' },
      testEnvironment: { userAgent: 'fake', windowWidth: 0, windowHeight: 0 },
      url: 'http://example.test/',
      timestamp: '2026-08-31T00:00:00.000Z',
    }
    expect(filterA11yViolations(fakeResults)).toEqual([])
  })
})
