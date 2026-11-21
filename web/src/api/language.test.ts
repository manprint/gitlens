import { afterEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'

import { client, LANGUAGE_HEADER } from './client'

describe('API language header (plan 006 F5)', () => {
  afterEach(async () => {
    vi.restoreAllMocks()
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-API-005 every request names the console language', async () => {
    const seen: (string | null)[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      seen.push(input instanceof Request ? input.headers.get(LANGUAGE_HEADER) : null)
      return Promise.resolve(Response.json([]))
    })

    await client.GET('/api/v1/findings')
    await setServerLanguage('it')
    await client.GET('/api/v1/findings')

    expect(seen).toEqual(['en', 'it'])
  })
})
