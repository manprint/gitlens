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
  server.listen({ onUnhandledRequest: 'error' })
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
  vi.useFakeTimers({ shouldAdvanceTime: false, now: NOW })

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
