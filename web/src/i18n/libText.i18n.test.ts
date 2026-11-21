import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { buildAshOption, otherFoldedLabel } from '@/components/charts/ash.options'
import { noIndexStats, noIndexStatsText } from '@/features/indexes/emptyText'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { formatDecimal } from '@/lib/format'
import { renameErrorMessage, validateDisplayName } from '@/lib/names'

// Texts built outside components (chart options, lib helpers) read in the console language too.
describe('lib texts in Italian (plan 006 F6)', () => {
  beforeAll(async () => {
    await setServerLanguage('it')
  })
  afterAll(async () => {
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-052 the ASH chart axis, tooltip and folded series read in Italian', () => {
    const points = [{ ts: '2026-09-01T10:00:00Z', value: 1.5, samples: 1200, ticks: 20 }]
    const option = buildAshOption(
      [
        { name: 'CPU', points },
        { name: 'other', points },
      ],
      { foldedCount: 3 },
    )
    expect(option.yAxis).toEqual(expect.objectContaining({ name: 'backend attivi medi' }))
    expect(otherFoldedLabel()).toBe('altro (accorpato)')

    const tooltip = (option.tooltip as { formatter: (params: unknown) => string }).formatter
    const html = tooltip([{ seriesName: 'CPU', value: ['2026-09-01T10:00:00Z', 1.5] }])
    expect(html).toContain(
      '<strong>CPU</strong>: media 1,50 backend attivi (campioni: 1.200, tick: 20)',
    )
    expect(html).toContain('altro (accorpato): 3 gruppi accorpati')
  })

  it('I18N-053 empty index lists, rename errors and decimals read in Italian', () => {
    expect(noIndexStats()).toMatch(/^Negli ultimi 15 minuti l’agent/)
    expect(noIndexStatsText({ database: 'app', reported: 3, foreignKeys: 0 })).toBe(
      'Il database app non ha indici nell’ultima raccolta.',
    )
    expect(validateDisplayName('x'.repeat(65))).toEqual({
      ok: false,
      error: 'Usa al massimo 64 caratteri.',
    })
    expect(renameErrorMessage({ kind: 'not_found' }, 'instance')).toBe(
      'L’istanza non esiste più. Ricarica la pagina.',
    )
    expect(formatDecimal(12345.5, 2)).toBe('12.345,50')
  })
})
