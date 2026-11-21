import { messages as english, type MessageKey } from '@/i18n/messages/en'
import { activeLocale, translate } from '@/i18n/store'

import { HELP_CATALOG } from './catalog'
import type { HelpAnchor } from './href'
import { topics as englishTopics } from './topics/en'
import type { HelpBlock, HelpLang, HelpSection, HelpTopic, Text } from './types'

export * from './types'

export { HELP_CATALOG, helpTopicTitle } from './catalog'

export { HELP_PATH, helpHref, type HelpAnchor } from './href'

function inCatalogOrder(topics: readonly HelpTopic[]): readonly HelpTopic[] {
  const rank = (topic: HelpTopic) => HELP_CATALOG.findIndex((entry) => entry.id === topic.id)
  return [...topics].sort((a, b) => rank(a) - rank(b))
}

/** The English help, in the order of the help menu (the catalog order). */
export const HELP_TOPICS: readonly HelpTopic[] = inCatalogOrder(englishTopics)

/** The other languages load with their first reader (chunk `help-<code>`). */
const LOADERS: Readonly<
  Record<Exclude<HelpLang, 'en'>, () => Promise<{ topics: readonly HelpTopic[] }>>
> = {
  it: () => import('./topics/it'),
}

const loaded = new Map<HelpLang, readonly HelpTopic[]>([['en', HELP_TOPICS]])

/** The help of a language when it is loaded, else undefined. */
export function loadedHelp(lang: HelpLang): readonly HelpTopic[] | undefined {
  return loaded.get(lang)
}

/** The help of a language; a chunk that fails to load leaves the English help. */
export async function loadHelp(lang: HelpLang): Promise<readonly HelpTopic[]> {
  const ready = loaded.get(lang)
  if (ready !== undefined) return ready
  try {
    const topics = inCatalogOrder((await LOADERS[lang as Exclude<HelpLang, 'en'>]()).topics)
    loaded.set(lang, topics)
    return topics
  } catch {
    return HELP_TOPICS
  }
}

export function findTopic(
  id: string | undefined,
  topics: readonly HelpTopic[] = HELP_TOPICS,
): HelpTopic | undefined {
  return topics.find((topic) => topic.id === id)
}

/** True when the anchor names an existing topic and, if given, one of its sections. */
export function anchorExists(
  anchor: HelpAnchor,
  topics: readonly HelpTopic[] = HELP_TOPICS,
): boolean {
  const [topicId, section] = anchor.split('#')
  const topic = findTopic(topicId, topics)
  if (topic === undefined) return false
  return section === undefined || allSections(topic).some((candidate) => candidate.id === section)
}

/** The sections of a topic and their subsections, in reading order. */
export function allSections(topic: HelpTopic): HelpSection[] {
  return topic.sections.flatMap((section) => [section, ...(section.subsections ?? [])])
}

const UI_MARK = /\{\{ui:([\w.-]+)\}\}/g

/** The message keys a help text cites with `{{ui:key}}`. */
export function uiKeys(text: string): string[] {
  return [...text.matchAll(UI_MARK)].map((match) => match[1]!)
}

/** True when `key` is a console message key. */
export function isMessageKey(key: string): key is MessageKey {
  return Object.hasOwn(english, key)
}

/** The text with each `{{ui:key}}` replaced by that console label in `locale`. */
export function resolveUi(text: string, locale: string = activeLocale()): string {
  return text.replace(UI_MARK, (mark, key: string) =>
    isMessageKey(key) ? translate(key, {}, locale) : mark,
  )
}

/** The plain text of a help text, without code, link and UI marks, for search. */
export function plainText(text: string, locale?: string): string {
  return resolveUi(text, locale)
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replaceAll('`', '')
}

export type InlinePart =
  | { kind: 'text'; value: string }
  | { kind: 'code'; value: string }
  | { kind: 'link'; value: string; href: string }

/** A help text split into plain text, `code` and `[label](href)` links, UI marks resolved. */
export function parseInline(source: string, locale?: string): InlinePart[] {
  const text = resolveUi(source, locale)
  const parts: InlinePart[] = []
  const pattern = /`([^`]+)`|\[([^\]]+)\]\(([^)\s]+)\)/g
  let last = 0
  for (const match of text.matchAll(pattern)) {
    const at = match.index
    if (at > last) parts.push({ kind: 'text', value: text.slice(last, at) })
    if (match[1] !== undefined) parts.push({ kind: 'code', value: match[1] })
    else parts.push({ kind: 'link', value: match[2]!, href: match[3]! })
    last = at + match[0].length
  }
  if (last < text.length) parts.push({ kind: 'text', value: text.slice(last) })
  return parts
}

/** Every text of a block, in reading order. */
export function blockTexts(block: HelpBlock): Text[] {
  switch (block.kind) {
    case 'text':
    case 'note':
      return [block.text]
    case 'steps':
      return [...block.items]
    case 'sql':
      return [block.label]
    case 'terms':
      return block.items.flatMap((item) => [
        item.term,
        item.meaning,
        ...(item.action ? [item.action] : []),
      ])
  }
}

/** Every text of a topic: titles, summary, page label and blocks. */
export function topicTexts(topic: HelpTopic): Text[] {
  return [
    topic.title,
    topic.summary,
    ...(topic.page ? [topic.page.label] : []),
    ...allSections(topic).flatMap((section) => [
      section.title,
      ...section.blocks.flatMap(blockTexts),
    ]),
  ]
}

export interface HelpHit {
  topic: HelpTopic
  section: HelpSection
  /** The first matching sentence. */
  excerpt: string
}

const MAX_HITS = 50

/** The sections whose title or text holds every word of the query. */
export function searchHelp(
  query: string,
  topics: readonly HelpTopic[] = HELP_TOPICS,
  locale?: string,
): HelpHit[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return []
  const hits: HelpHit[] = []
  for (const topic of topics) {
    for (const section of allSections(topic)) {
      const texts = [section.title, ...section.blocks.flatMap(blockTexts)].map((text) =>
        plainText(text, locale),
      )
      const haystack = texts.join(' ').toLowerCase()
      if (!words.every((word) => haystack.includes(word))) continue
      const sentences = texts.flatMap((text) => text.split(/(?<=[.:;])\s+/))
      const excerpt =
        sentences.find((sentence) => sentence.toLowerCase().includes(words[0]!)) ??
        sentences[0] ??
        ''
      hits.push({ topic, section, excerpt })
      if (hits.length >= MAX_HITS) return hits
    }
  }
  return hits
}

/** The topic and section an anchor names, for a link inside the help. */
export function resolveAnchor(
  anchor: HelpAnchor,
  topics: readonly HelpTopic[] = HELP_TOPICS,
): { topic: HelpTopic; section?: HelpSection } | undefined {
  const [topicId, sectionId] = anchor.split('#')
  const topic = findTopic(topicId, topics)
  if (topic === undefined) return undefined
  const section = allSections(topic).find((candidate) => candidate.id === sectionId)
  return section === undefined ? { topic } : { topic, section }
}
