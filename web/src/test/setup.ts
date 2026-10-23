import '@testing-library/jest-dom/vitest'

import { cleanup } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, beforeEach, vi } from 'vitest'

import { server } from './msw/server'
import { NOW } from './time'

/**
 * Known-benign console messages may be added here with a narrowly scoped
 * reason. React key and act warnings intentionally are not allow-listed.
 */
export const BENIGN_CONSOLE_MESSAGES: string[] = []

beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' })
})

afterEach(() => {
  server.resetHandlers()
})

afterAll(() => {
  server.close()
})

afterEach(() => {
  cleanup()
})

beforeEach(() => {
  // setImmediate stays real: MSW 3 no longer works around fake timers, and
  // part of its mocked-response delivery runs on setImmediate. Faking it parks
  // every mocked response until a test happens to advance the clock.
  // Application code never uses setImmediate, so nothing the suites assert on
  // depends on it being controlled. See flushNetwork() in network.ts.
  vi.useFakeTimers({
    shouldAdvanceTime: false,
    now: NOW,
    toNotFake: ['setImmediate', 'clearImmediate'],
  })

  for (const method of ['error', 'warn'] as const) {
    vi.spyOn(console, method).mockImplementation((...args) => {
      const message = args.map((argument) => String(argument)).join(' ')
      if (BENIGN_CONSOLE_MESSAGES.some((allowed) => message.includes(allowed))) {
        return
      }
      throw new Error(`Unexpected console.${method}: ${message}`)
    })
  }
})

afterEach(() => {
  vi.useRealTimers()
})
