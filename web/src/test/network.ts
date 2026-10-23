import { act } from '@testing-library/react'
import { vi } from 'vitest'

/**
 * Let mocked network responses, and the work they trigger, reach the code
 * under test.
 *
 * MSW 3 delivers a mocked response across several turns of the real event
 * loop (setImmediate is deliberately left unfaked, see setup.ts), and part of
 * that delivery runs inside a fake-timer callback. A `setTimeout(fn, 0)`
 * scheduled while the fake clock is ticking is due one millisecond later, not
 * now — that is how the fake-timer clock avoids a zero-delay loop — so the
 * TanStack Query notification that follows a settled mutation is never run by
 * `advanceTimersByTimeAsync(0)`. Each turn therefore advances the fake clock
 * by one millisecond, drains React updates inside act(), and yields one real
 * event-loop turn.
 */
export async function flushNetwork(turns = 10): Promise<void> {
  for (let turn = 0; turn < turns; turn += 1) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
      await new Promise<void>((resolve) => {
        setImmediate(resolve)
      })
    })
  }
}
