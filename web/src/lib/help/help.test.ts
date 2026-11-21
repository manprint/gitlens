import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { messages as english } from '@/i18n/messages/en'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { FLAG_GUIDE, FLAG_LABELS, type FlagLabel } from '@/lib/indexGuide'

import {
  HELP_CATALOG,
  HELP_LANGS,
  HELP_TOPICS,
  allSections,
  anchorExists,
  blockTexts,
  helpHref,
  loadHelp,
  parseInline,
  resolveUi,
  searchHelp,
  topicTexts,
  uiKeys,
  type HelpBlock,
  type HelpLang,
  type HelpSection,
  type HelpTopic,
  type Text,
} from './index'

const sources = import.meta.glob<string>(
  [
    '/src/**/*.ts',
    '/src/**/*.tsx',
    '!/src/**/*.test.ts',
    '!/src/**/*.test.tsx',
    '!/src/lib/help/**',
  ],
  { query: '?raw', import: 'default', eager: true },
)

const APP_PATHS =
  /^\/(?:$|\?|queries|indexes|findings|alerts|events|settings|instances|clusters|help)/

const HELP: Record<HelpLang, readonly HelpTopic[]> = { en: HELP_TOPICS, it: [] }

beforeAll(async () => {
  for (const lang of HELP_LANGS) HELP[lang] = await loadHelp(lang)
})

/** The code, link and UI marks of a text, in order: what every language keeps. */
function marks(text: string): string[] {
  const ui = uiKeys(text).map((key) => `ui:${key}`)
  const inline = parseInline(text.replace(/\{\{ui:[\w.-]+\}\}/g, ''))
    .filter((part) => part.kind !== 'text')
    .map((part) => (part.kind === 'link' ? `link:${part.href}` : `code:${part.value}`))
  return [...inline, ...ui]
}

function everyText(lang: HelpLang): { where: string; text: Text }[] {
  return HELP[lang].flatMap((topic) =>
    topicTexts(topic).map((text, index) => ({ where: `${lang} ${topic.id} #${index}`, text })),
  )
}

/** The shape of a block without its words. */
function blockSkeleton(block: HelpBlock): unknown {
  const texts = blockTexts(block).map(marks)
  switch (block.kind) {
    case 'sql':
      return { kind: block.kind, code: block.code, texts }
    case 'note':
      return { kind: block.kind, tone: block.tone, texts }
    case 'terms':
      return {
        kind: block.kind,
        actions: block.items.map((item) => item.action !== undefined),
        texts,
      }
    case 'steps':
    case 'text':
      return { kind: block.kind, texts }
  }
}

function sectionSkeleton(section: HelpSection): unknown {
  return {
    id: section.id,
    title: marks(section.title),
    blocks: section.blocks.map(blockSkeleton),
    subsections: section.subsections?.map(sectionSkeleton),
  }
}

function topicSkeleton(topic: HelpTopic): unknown {
  return {
    id: topic.id,
    group: topic.group,
    page: topic.page?.path,
    marks: [topic.title, topic.summary, ...(topic.page ? [topic.page.label] : [])].map(marks),
    sections: topic.sections.map(sectionSkeleton),
  }
}

describe('Inline Help content', () => {
  it('HELP-001 every topic matches its catalog entry: id, group and title', () => {
    for (const lang of HELP_LANGS) {
      const ids = HELP[lang].map((topic) => topic.id)
      expect(new Set(ids).size).toBe(ids.length)
      for (const topic of HELP[lang]) {
        const entry = HELP_CATALOG.find((candidate) => candidate.id === topic.id)
        expect(entry, topic.id).toBeDefined()
        expect(topic.group, topic.id).toBe(entry!.group)
        expect(topic.title, `${lang} ${topic.id}`).toBe(entry!.title[lang])
      }
    }
  })

  it('HELP-002 section anchors are unique within a topic and kebab-case', () => {
    for (const topic of HELP_TOPICS) {
      const ids = allSections(topic).map((section) => section.id)
      expect(new Set(ids).size, topic.id).toBe(ids.length)
      for (const id of ids) expect(id, topic.id).toMatch(/^[a-z0-9]+(?:-[a-z0-9]+)*$/)
      expect(topic.sections.length, topic.id).toBeGreaterThan(0)
    }
  })

  it('HELP-003 every text is written in every language, with no placeholder', () => {
    for (const lang of HELP_LANGS) {
      for (const { where, text } of everyText(lang)) {
        const value = text.trim()
        expect(value, where).not.toBe('')
        expect(value, where).not.toMatch(/\bTODO\b|\bTBD\b|\.\.\.|\bN\/A\b|lorem/i)
      }
    }
  })

  it('I18N-005 every language has the English skeleton: sections, blocks, SQL and marks', () => {
    for (const lang of HELP_LANGS) {
      expect(HELP[lang].map(topicSkeleton), lang).toEqual(HELP.en.map(topicSkeleton))
    }
  })

  it('I18N-006 every {{ui:key}} names a console message without arguments', () => {
    for (const lang of HELP_LANGS) {
      for (const { where, text } of everyText(lang)) {
        for (const key of uiKeys(text)) {
          expect(Object.hasOwn(english, key), `${where}: ${key}`).toBe(true)
          expect(english[key as keyof typeof english], `${where}: ${key}`).not.toContain('{')
        }
      }
    }
  })

  it('HELP-004 the index flags read as their badge tooltips (FLAG_GUIDE), in every language', () => {
    for (const lang of HELP_LANGS) {
      const topic = HELP[lang].find((candidate) => candidate.id === 'indexes')!
      const terms = allSections(topic)
        .filter((section) => section.id === 'flags-problems' || section.id === 'flags-kinds')
        .flatMap((section) => section.blocks)
        .flatMap((block) => (block.kind === 'terms' ? block.items : []))
      expect(terms.map((item) => item.term).sort(), lang).toEqual([...FLAG_LABELS].sort())
      for (const item of terms) {
        const guide = FLAG_GUIDE[item.term as FlagLabel]
        expect(item.meaning, `${lang} ${item.term}`).toBe(guide.meaning[lang])
        expect(item.action, `${lang} ${item.term}`).toBe(guide.action[lang])
      }
    }
  })

  it('HELP-005 every link inside the help points to an existing help section or console page', () => {
    for (const { where, text } of everyText('en')) {
      for (const part of parseInline(text)) {
        if (part.kind !== 'link' || !part.href.startsWith('/')) continue
        if (part.href.startsWith('/help/')) {
          expect(anchorExists(part.href.slice('/help/'.length)), `${where}: ${part.href}`).toBe(
            true,
          )
        } else {
          expect(part.href, where).toMatch(APP_PATHS)
        }
      }
    }
  })

  it('HELP-005b every chapter opens a console page, with only the server as a parameter', () => {
    for (const topic of HELP_TOPICS) {
      if (!topic.page) continue
      expect(topic.page.path, topic.id).toMatch(APP_PATHS)
      expect(topic.page.path.replace(':instanceId', 'x'), topic.id).not.toContain(':')
    }
  })

  it('HELP-006 every block has content: steps, terms and SQL are never empty', () => {
    for (const topic of HELP_TOPICS) {
      for (const section of allSections(topic)) {
        expect(section.blocks.length, `${topic.id}#${section.id}`).toBeGreaterThan(0)
        for (const block of section.blocks) {
          if (block.kind === 'steps' || block.kind === 'terms') {
            expect(block.items.length, `${topic.id}#${section.id}`).toBeGreaterThan(0)
          }
          if (block.kind === 'sql')
            expect(block.code.trim(), `${topic.id}#${section.id}`).not.toBe('')
        }
      }
    }
  })

  it('HELP-007 every Help link of the console opens an existing section', () => {
    const anchors = Object.entries(sources).flatMap(([file, source]) =>
      [...source.matchAll(/<HelpLink[^>]*?\banchor="([^"]+)"/g)].map((match) => ({
        file,
        anchor: match[1]!,
      })),
    )
    expect(anchors.length).toBeGreaterThan(0)
    for (const { file, anchor } of anchors)
      expect(anchorExists(anchor), `${file}: ${anchor}`).toBe(true)
  })

  it('HELP-008 every chapter of the catalog is written, in every language', () => {
    for (const lang of HELP_LANGS)
      expect(
        HELP[lang].map((topic) => topic.id),
        lang,
      ).toEqual(HELP_CATALOG.map((entry) => entry.id))
  })

  it('UI-MCP-002 the MCP chapter exists in en and it, with the same sections', () => {
    const sections = (lang: HelpLang) =>
      allSections(HELP[lang].find((topic) => topic.id === 'mcp')!).map((section) => section.id)
    expect(sections('en')).toEqual(['overview', 'enable', 'connect', 'tools', 'limits', 'security'])
    expect(sections('it')).toEqual(sections('en'))
  })
})

describe('Inline Help helpers', () => {
  beforeAll(async () => {
    await setServerLanguage('it')
  })
  afterAll(async () => {
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('HELP-009 inline marks split into text, code and links', () => {
    expect(parseInline('Open [Cleanup](/help/indexes#cleanup), see `top_n`.')).toEqual([
      { kind: 'text', value: 'Open ' },
      { kind: 'link', value: 'Cleanup', href: '/help/indexes#cleanup' },
      { kind: 'text', value: ', see ' },
      { kind: 'code', value: 'top_n' },
      { kind: 'text', value: '.' },
    ])
    expect(helpHref('indexes#flags')).toBe('/help/indexes#flags')
    expect(helpHref('indexes')).toBe('/help/indexes')
  })

  it('HELP-011 {{ui:key}} reads as the console label of the language; an unknown key stays', () => {
    expect(resolveUi('Open {{ui:help.page.chapters}}.', 'en')).toBe('Open Chapters.')
    expect(resolveUi('Apri {{ui:help.page.chapters}}.', 'it')).toBe('Apri Capitoli.')
    expect(resolveUi('{{ui:no.such.key}}', 'it')).toBe('{{ui:no.such.key}}')
    expect(parseInline('See `x` in {{ui:help.page.chapters}}', 'it')).toEqual([
      { kind: 'text', value: 'See ' },
      { kind: 'code', value: 'x' },
      { kind: 'text', value: ' in Capitoli' },
    ])
  })

  it('HELP-010 search matches every word, in the help of the language', () => {
    expect(searchHelp('')).toEqual([])
    const found = (query: string, lang: HelpLang) =>
      searchHelp(query, HELP[lang], lang).some(
        (hit) => hit.topic.id === 'indexes' && hit.section.id === 'dead-rows',
      )
    expect(found('dead rows', 'en')).toBe(true)
    expect(found('tab dead rows', 'it')).toBe(true)
    expect(searchHelp('zzzz-no-such-word')).toEqual([])
  })
})
