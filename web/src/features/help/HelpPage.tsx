import { BookOpen, ExternalLink, Hash, Search } from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'

import { CopyButton } from '@/components/layout/CopyButton'
import { PageHeader } from '@/components/layout/PageHeader'
import { Callout } from '@/components/ui/callout'
import { Input } from '@/components/ui/input'
import { useLocale, useT, type Translate } from '@/i18n/react'
import {
  HELP_GROUP_TITLE,
  findTopic,
  helpHref,
  helpLang,
  loadHelp,
  loadedHelp,
  parseInline,
  plainText,
  resolveUi,
  searchHelp,
  type HelpBlock,
  type HelpGroup,
  type HelpLang,
  type HelpSection,
  type HelpTopic,
} from '@/lib/help'
import { readLastInstance } from '@/lib/lastInstance'
import { cn } from '@/lib/utils'

/**
 * The console page of a chapter. `:instanceId` is the server opened last; when
 * none was opened yet, the link goes to Fleet to pick one.
 */
function pageLink(page: NonNullable<HelpTopic['page']>, t: Translate) {
  const label = resolveUi(page.label)
  if (!page.path.includes(':instanceId')) return { to: page.path, label }
  const instanceId = readLastInstance()
  if (instanceId === null) return { to: '/', label: t('help.page.openFleet') }
  return {
    to: page.path.replace(':instanceId', encodeURIComponent(instanceId)),
    label,
  }
}

const GROUPS: readonly HelpGroup[] = ['start', 'monitoring', 'instance', 'alerting', 'admin']

/** The DOM id of a section: prefixed, so a section id never clashes with the console's own ids. */
function domId(sectionId: string): string {
  return `help-${sectionId}`
}

/** A help text with its marks: code, links and UI labels in the console language. */
function Inline({ text }: { text: string }) {
  const locale = useLocale()
  return (
    <>
      {parseInline(text, locale).map((part, index) => {
        if (part.kind === 'text') return part.value
        if (part.kind === 'code') {
          return (
            <code key={index} className="bg-muted rounded px-1 py-0.5 font-mono text-[0.85em]">
              {part.value}
            </code>
          )
        }
        return part.href.startsWith('/') ? (
          <Link key={index} className="text-primary underline underline-offset-2" to={part.href}>
            {part.value}
          </Link>
        ) : (
          <a
            key={index}
            className="text-primary underline underline-offset-2"
            href={part.href}
            rel="noopener noreferrer"
            target="_blank"
          >
            {part.value}
          </a>
        )
      })}
    </>
  )
}

function Block({ block }: { block: HelpBlock }) {
  const t = useT()
  switch (block.kind) {
    case 'text':
      return (
        <p className="leading-relaxed">
          <Inline text={block.text} />
        </p>
      )
    case 'note':
      return (
        <Callout role="note" variant={block.tone === 'warning' ? 'warning' : 'default'}>
          <Inline text={block.text} />
        </Callout>
      )
    case 'steps':
      return (
        <ol className="list-decimal space-y-1.5 pl-6 leading-relaxed">
          {block.items.map((item, index) => (
            <li key={index}>
              <Inline text={item} />
            </li>
          ))}
        </ol>
      )
    case 'sql':
      return (
        <figure className="space-y-1.5">
          <figcaption className="flex items-center justify-between gap-3 text-sm font-medium">
            <span>
              <Inline text={block.label} />
            </span>
            <CopyButton subject="SQL" text={block.code} />
          </figcaption>
          <pre className="bg-muted overflow-x-auto rounded-md p-3 text-xs leading-relaxed">
            <code>{block.code}</code>
          </pre>
        </figure>
      )
    case 'terms':
      return (
        <dl className="divide-y rounded-lg border">
          {block.items.map((item, index) => (
            <div key={index} className="grid gap-1 px-4 py-3 md:grid-cols-[15rem_1fr] md:gap-4">
              <dt className="font-medium">
                <Inline text={item.term} />
              </dt>
              <dd className="space-y-1 leading-relaxed">
                <p>
                  <Inline text={item.meaning} />
                </p>
                {item.action ? (
                  <p>
                    <span className="font-medium">{t('help.page.whatToDo')}</span>{' '}
                    <Inline text={item.action} />
                  </p>
                ) : null}
              </dd>
            </div>
          ))}
        </dl>
      )
  }
}

function SectionView({ section, level }: { section: HelpSection; level: 2 | 3 }) {
  const t = useT()
  const Heading = level === 2 ? 'h2' : 'h3'
  const titleId = `${domId(section.id)}-title`
  return (
    <section aria-labelledby={titleId} className="scroll-mt-24 space-y-3" id={domId(section.id)}>
      <Heading
        className={cn(
          'group flex flex-wrap items-baseline gap-x-2 font-semibold tracking-tight',
          level === 2 ? 'border-b pb-2 text-xl' : 'pt-2 text-base',
        )}
        id={titleId}
      >
        <span>
          <Inline text={section.title} />
        </span>
        <Link
          aria-label={t('help.page.sectionLink', { title: section.title })}
          className="text-muted-foreground hover:text-primary opacity-60 group-hover:opacity-100"
          to={{ hash: section.id }}
        >
          <Hash aria-hidden="true" className="size-3.5" />
        </Link>
      </Heading>
      {section.blocks.map((block, index) => (
        <Block key={index} block={block} />
      ))}
      {section.subsections?.map((sub) => (
        <SectionView key={sub.id} level={3} section={sub} />
      ))}
    </section>
  )
}

function Contents({ topic }: { topic: HelpTopic }) {
  const t = useT()
  return (
    <nav aria-label={t('help.page.onThisPage')} className="bg-card rounded-lg border p-4 text-sm">
      <p className="mb-2 font-semibold">{t('help.page.onThisPage')}</p>
      <ol className="space-y-1">
        {topic.sections.map((section) => (
          <li key={section.id}>
            <Link className="text-primary hover:underline" to={{ hash: section.id }}>
              <Inline text={section.title} />
            </Link>
            {section.subsections && section.subsections.length > 0 ? (
              <ol className="mt-1 space-y-0.5 border-l pl-3">
                {section.subsections.map((sub) => (
                  <li key={sub.id}>
                    <Link
                      className="text-muted-foreground hover:text-primary hover:underline"
                      to={{ hash: sub.id }}
                    >
                      <Inline text={sub.title} />
                    </Link>
                  </li>
                ))}
              </ol>
            ) : null}
          </li>
        ))}
      </ol>
    </nav>
  )
}

function TopicView({ topic, lang }: { topic: HelpTopic; lang: HelpLang }) {
  const t = useT()
  const page = topic.page ? pageLink(topic.page, t) : null
  return (
    <article className="max-w-6xl min-w-0 space-y-8" lang={lang}>
      <div className="space-y-4">
        {page ? (
          <Link
            className="text-primary inline-flex items-center gap-1.5 text-sm font-medium hover:underline"
            to={page.to}
          >
            <ExternalLink aria-hidden="true" className="size-4" />
            {page.label}
          </Link>
        ) : null}
        <Contents topic={topic} />
      </div>
      {topic.sections.map((section) => (
        <SectionView key={section.id} level={2} section={section} />
      ))}
    </article>
  )
}

function HomeView({ topics, lang }: { topics: readonly HelpTopic[]; lang: HelpLang }) {
  return (
    <div className="min-w-0 space-y-6" lang={lang}>
      {GROUPS.map((group) => {
        const inGroup = topics.filter((topic) => topic.group === group)
        if (inGroup.length === 0) return null
        return (
          <section key={group} aria-labelledby={`help-group-${group}`} className="space-y-3">
            <h2 className="text-lg font-semibold" id={`help-group-${group}`}>
              {HELP_GROUP_TITLE[group][lang]}
            </h2>
            <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {inGroup.map((topic) => (
                <li key={topic.id} className="bg-card rounded-lg border p-4">
                  <Link
                    className="text-primary font-medium hover:underline"
                    to={helpHref(topic.id)}
                  >
                    <Inline text={topic.title} />
                  </Link>
                  <p className="text-muted-foreground mt-1 text-sm leading-relaxed">
                    <Inline text={topic.summary} />
                  </p>
                </li>
              ))}
            </ul>
          </section>
        )
      })}
    </div>
  )
}

function SearchResults({
  query,
  topics,
  lang,
  onPick,
}: {
  query: string
  topics: readonly HelpTopic[]
  lang: HelpLang
  onPick: () => void
}) {
  const t = useT()
  const locale = useLocale()
  const hits = useMemo(() => searchHelp(query, topics, locale), [query, topics, locale])
  return (
    <section aria-label={t('help.page.search')} className="min-w-0 space-y-3" lang={lang}>
      <p aria-live="polite" className="text-muted-foreground text-sm">
        {t('help.page.hits', { count: hits.length })}
      </p>
      {hits.length === 0 ? (
        <p>{t('help.page.noHits')}</p>
      ) : (
        <ol className="space-y-2">
          {hits.map((hit) => (
            <li key={`${hit.topic.id}#${hit.section.id}`} className="bg-card rounded-lg border p-3">
              <Link
                className="text-primary font-medium hover:underline"
                onClick={onPick}
                to={helpHref(`${hit.topic.id}#${hit.section.id}`)}
              >
                <Inline text={hit.topic.title} /> › <Inline text={hit.section.title} />
              </Link>
              <p className="text-muted-foreground mt-1 text-sm">{hit.excerpt}</p>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

function ChapterList({
  topics,
  current,
  lang,
}: {
  topics: readonly HelpTopic[]
  current?: string
  lang: HelpLang
}) {
  const t = useT()
  return (
    <nav aria-label={t('help.page.chapters')} className="space-y-4 text-sm">
      {GROUPS.map((group) => {
        const inGroup = topics.filter((topic) => topic.group === group)
        if (inGroup.length === 0) return null
        return (
          <div key={group}>
            <p className="text-muted-foreground mb-1 text-xs font-semibold tracking-wider uppercase">
              {HELP_GROUP_TITLE[group][lang]}
            </p>
            <ul className="space-y-0.5">
              {inGroup.map((topic) => (
                <li key={topic.id}>
                  <Link
                    aria-current={topic.id === current ? 'page' : undefined}
                    className={cn(
                      'hover:bg-muted block rounded-md px-2 py-1',
                      topic.id === current && 'bg-primary/10 text-primary font-medium',
                    )}
                    to={helpHref(topic.id)}
                  >
                    <Inline text={topic.title} />
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        )
      })}
    </nav>
  )
}

/**
 * The help in the console language (English when the help lacks it). Another
 * language loads on first use; until then the page shows a loading line.
 */
function useHelp(): { lang: HelpLang; topics: readonly HelpTopic[] | undefined } {
  const lang = helpLang(useLocale())
  const [, setLoaded] = useState(0)
  const topics = loadedHelp(lang)
  useEffect(() => {
    if (topics !== undefined) return
    let live = true
    void loadHelp(lang).then(() => {
      if (live) setLoaded((n) => n + 1)
    })
    return () => {
      live = false
    }
  }, [lang, topics])
  return { lang, topics }
}

function useScrollToAnchor(enabled: boolean, topicId: string | undefined) {
  const { hash } = useLocation()
  useEffect(() => {
    if (!enabled) return
    const id = decodeURIComponent(hash.slice(1))
    if (id === '') {
      window.scrollTo?.({ top: 0 })
      return
    }
    document.getElementById(domId(id))?.scrollIntoView?.({ block: 'start' })
  }, [enabled, hash, topicId])
}

export default function HelpPage() {
  const t = useT()
  const locale = useLocale()
  const { topicId } = useParams()
  const { lang, topics } = useHelp()
  const [query, setQuery] = useState('')
  const topic = topics === undefined ? undefined : findTopic(topicId, topics)
  const searching = query.trim() !== ''
  useScrollToAnchor(!searching && topics !== undefined, topicId)

  let body: ReactNode
  if (topics === undefined)
    body = (
      <p aria-live="polite" className="text-muted-foreground text-sm">
        {t('help.page.loading')}
      </p>
    )
  else if (searching)
    body = <SearchResults lang={lang} onPick={() => setQuery('')} query={query} topics={topics} />
  else if (topic) body = <TopicView lang={lang} topic={topic} />
  else if (topicId !== undefined)
    body = <Callout variant="warning">{t('help.page.notFound')}</Callout>
  else body = <HomeView lang={lang} topics={topics} />

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow={t('shell.nav.inlineHelp')}
        icon={BookOpen}
        subtitle={topic ? <Inline text={topic.summary} /> : t('help.page.summary')}
        title={topic ? plainText(topic.title, locale) : t('help.page.title')}
      />
      <div className="grid gap-6 lg:grid-cols-[15rem_minmax(0,1fr)]">
        <aside className="space-y-4 lg:sticky lg:top-4 lg:max-h-[calc(100vh-10rem)] lg:self-start lg:overflow-y-auto">
          <div className="relative">
            <Search
              aria-hidden="true"
              className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
            />
            <Input
              aria-label={t('help.page.search')}
              className="pl-8"
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t('help.page.search')}
              type="search"
              value={query}
            />
          </div>
          {topics === undefined ? null : (
            <ChapterList lang={lang} topics={topics} {...(topic ? { current: topic.id } : {})} />
          )}
        </aside>
        {body}
      </div>
    </div>
  )
}
