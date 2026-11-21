import { afterEach, describe, expect, it } from 'vitest'

import { CAPTIONS, captionOf, columnHelp, columnHelpOf, type CaptionId } from '@/lib/captions'
import { describeEvent } from '@/lib/events'
import { glossary, glossaryOf, type Term } from '@/lib/glossary'
import { resetPreferences, updatePreferences } from '@/lib/preferences'
import { needsText, ruleText } from '@/lib/ruleText'
import { formatUnit } from '@/lib/units'

import { domain } from './domain/it'
import { PSEUDO_LOCALE } from './locales'
import { pseudoLocalize } from './pseudo'
import { refreshLocale, setServerLanguage } from './store'

describe('domain texts follow the console language (plan 006 F4)', () => {
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-050 captions, rule sentences, units and glossary read in Italian', async () => {
    expect(captionOf('overview.tps').what).toBe(CAPTIONS['overview.tps'].what)
    await setServerLanguage('it')

    expect(captionOf('overview.tps').what).toBe(
      'Transazioni confermate al secondo nel database selezionato.',
    )
    expect(ruleText('archive.disabled')).toBe(domain.rules['archive.disabled'])
    expect(needsText([])).toBe(domain.noNeeds)
    expect(formatUnit('rows', 2)).toBe('righe')
    expect(formatUnit('rows', 1)).toBe('riga')
    expect(formatUnit('ms', 3)).toBe('ms')
    expect(glossaryOf('WAL').short).toBe(domain.glossary.WAL.short)
    const firstColumn = Object.keys(columnHelp)[0] as keyof typeof columnHelp
    expect(columnHelpOf(firstColumn)).toBe(domain.columnHelp[firstColumn])
    expect(describeEvent('deadlock_captured')).toMatchObject({
      severity: 'warning',
      title: 'Deadlock catturato',
    })
    expect(describeEvent('not_a_type').title).toBe('Tipo di evento sconosciuto')
  })

  it('I18N-050 the pseudo-locale marks domain texts', async () => {
    updatePreferences({ language: PSEUDO_LOCALE })
    await refreshLocale()

    expect(captionOf('overview.tps').what).toBe(pseudoLocalize(CAPTIONS['overview.tps'].what))
    expect(formatUnit('rows', 2)).toBe(pseudoLocalize('rows'))
  })

  it('I18N-051 every Italian caption and glossary entry has the fields English has', () => {
    for (const id of Object.keys(CAPTIONS) as CaptionId[]) {
      expect(Object.keys(domain.captions[id]).sort(), id).toEqual(Object.keys(CAPTIONS[id]).sort())
      for (const text of Object.values(domain.captions[id]) as string[]) {
        expect(text.trim(), id).not.toBe('')
      }
    }
    for (const term of Object.keys(glossary) as Term[]) {
      expect(Object.keys(domain.glossary[term]).sort(), term).toEqual(
        Object.keys(glossary[term]).sort(),
      )
    }
    for (const text of [
      ...Object.values(domain.columnHelp),
      ...Object.values(domain.rules),
      ...Object.values(domain.needs),
    ]) {
      expect(text.trim()).not.toBe('')
    }
  })
})
