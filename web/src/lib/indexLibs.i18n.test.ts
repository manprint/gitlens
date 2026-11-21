import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { base } from '@/test/fixtures/getIndexSuggestions'

import { buildIndexRows } from './indexAnalysis'
import { buildCleanup } from './indexCleanup'
import { flagTitle } from './indexGuide'
import type { IndexSuggestion } from './indexSuggestions'
import { noteLabel, verdictLabel } from './indexSuggestions'
import { buildMissingIndexes } from './missingIndexes'

const FIRST = base.suggestions[0]! as unknown as IndexSuggestion

function suggestion(extra: Partial<IndexSuggestion> = {}): IndexSuggestion {
  return { ...FIRST, verdict: 'strong', confidence: 'high', ...extra }
}

function missing(suggestions: IndexSuggestion[]) {
  return buildMissingIndexes({ suggestions, findings: [], tables: [], database: '' })
}

function invalidIndex() {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    indexrelname: 'bad',
    index_bytes: 1000,
    idx_scan_rate: 1,
    is_primary: false,
    is_unique: false,
    is_valid: false,
    last_scan_status: 'scanned',
    definition: 'CREATE INDEX bad ON public.orders USING btree (customer_id)',
  }
}

describe('index libs i18n (plan 006 F3)', () => {
  beforeEach(async () => {
    await setServerLanguage('it')
  })
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-039 the four index libs answer in Italian when the server language is it', () => {
    const [strong] = missing([suggestion()])
    expect(strong!.steps.map((step) => step.title)).toEqual([
      'Verifica',
      'Crea',
      'Conferma',
      'Annulla',
    ])
    expect(strong!.why).toContain('senza indice')

    const [low] = missing([suggestion({ read_cost_ms_per_s: 0.2, weight_pct: 0.5 })])
    expect(low!.why).toContain('Il guadagno è piccolo')

    expect(noteLabel('small_table')).toBe(
      'La tabella ha meno di 1.000 righe: una scansione sequenziale è di solito veloce quanto un indice.',
    )
    expect(verdictLabel('strong')).toBe('Forte')
    expect(flagTitle('invalid')).toContain('Cosa fare:')

    const [candidate] = buildCleanup(
      buildIndexRows([invalidIndex()], [] as never),
      new Map<string, number>(),
      null,
    )
    expect(candidate!.why).toBe(
      'L’indice non è valido: le query non possono usarlo, e una costruzione fallita lo lascia lì.',
    )
    expect(candidate!.whyEn).toBe(
      'The index is invalid: queries cannot use it, and a failed build leaves it behind.',
    )
  })
})
