import { useId, useState } from 'react'

import {
  useDeleteNotificationChannel,
  useNotificationChannels,
  useSetNotificationChannel,
  useSetNotificationLanguage,
  useTestNotificationChannel,
} from '@/api/queries'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { localeCodes, localeInfo, PSEUDO_LOCALE } from '@/i18n/locales'
import type { MessageKey } from '@/i18n/messages/en'
import { useT } from '@/i18n/react'
import { resolveLocale } from '@/i18n/store'
import {
  addressFieldLabel,
  addressProblem,
  channelChangeFailureMessage,
  channelInstructions,
  channelLabel,
  channelSourceLabel,
  notificationChannelInfo,
  testFailureMessage,
  testResultText,
  type NotificationChannel,
  type NotificationChannelKind,
} from '@/lib/notificationChannels'
import { cn } from '@/lib/utils'

/** What Send test does (I-16), referenced by every Send test button. */
const SEND_TEST_HELP = 'settings.notifications.sendTestHelp' satisfies MessageKey

/** What the URL form does (I-16), referenced by every URL field. */
const URL_HELP = 'settings.notifications.urlHelp' satisfies MessageKey

/** The last test send of one channel. */
type TestState = { phase: 'pending' } | { phase: 'done'; ok: boolean; text: string }

/** The last change of one channel's stored URL or language. */
type ChangeState = { phase: 'pending' } | { phase: 'done'; ok: boolean; text: string }

/** The languages a channel can write in; the server knows no pseudo-language. */
function channelLanguages(): string[] {
  return localeCodes().filter((code) => code !== PSEUDO_LOCALE)
}

/** The select's value for the server default. */
const SERVER_LANGUAGE = ''

function ChannelRow({
  channel,
  state,
  change,
  languageChange,
  onTest,
  onSave,
  onRemove,
  onLanguage,
  helpId,
  urlHelpId,
}: {
  channel: NotificationChannel
  state: TestState | undefined
  change: ChangeState | undefined
  languageChange: ChangeState | undefined
  onTest: () => void
  /** Sets the language of the channel's messages; null is the server default. */
  onLanguage: (language: string | null) => void
  /** Stores the URL; resolves true when the server stored it. */
  onSave: (url: string) => Promise<boolean>
  onRemove: () => void
  helpId: string
  urlHelpId: string
}) {
  const t = useT()
  const info = notificationChannelInfo(channel.kind)
  const id = useId().replaceAll(':', '')
  const ids = {
    instructions: `${id}-instructions`,
    result: `${id}-result`,
    url: `${id}-url`,
    change: `${id}-change`,
    language: `${id}-language`,
    languageChange: `${id}-language-change`,
    chanHead: `${id}-h3`,
  }
  const pending = state?.phase === 'pending'
  const changing = change?.phase === 'pending'
  // The URL being typed: never filled from the server, cleared once stored.
  const [draft, setDraft] = useState('')
  const [problem, setProblem] = useState<string | null>(null)
  const stored = channel.source === 'ui'

  async function save() {
    // Spaces around a pasted URL are dropped; the server refuses any left inside.
    const url = draft.trim()
    const found = addressProblem(info, url)
    setProblem(found)
    if (found !== null) return
    if (await onSave(url)) setDraft('')
  }

  // A typed address is checked as it is typed, so Save waits for one the server takes.
  const typed = draft.trim()
  const liveProblem = typed === '' ? null : addressProblem(info, typed)
  const shownProblem = problem ?? liveProblem
  const placeholder = stored
    ? t('settings.notifications.urlPlaceholderStored')
    : info.address === 'routingKey'
      ? t('settings.notifications.routingKeyPlaceholder')
      : info.address === 'recipients'
        ? t('settings.notifications.recipientsPlaceholder')
        : t('settings.notifications.urlPlaceholder')

  const changeText =
    shownProblem ??
    (change?.phase === 'done' ? change.text : changing ? t('settings.notifications.saving') : '')
  const changeFailed = shownProblem !== null || (change?.phase === 'done' && !change.ok)
  const languageText =
    languageChange?.phase === 'done'
      ? languageChange.text
      : languageChange?.phase === 'pending'
        ? t('settings.notifications.saving')
        : ''

  return (
    <li
      aria-labelledby={ids.chanHead}
      className="grid gap-2 border-b py-3 first:pt-0 last:border-b-0 last:pb-0 min-[1600px]:grid-cols-[19rem_14rem_minmax(0,1fr)_minmax(0,22rem)] min-[1600px]:items-center min-[1600px]:gap-x-4"
      data-testid={`notification-channel-${channel.kind}`}
    >
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-foreground text-sm font-medium" id={ids.chanHead}>
          {channelLabel(info)}
        </h3>
        <Badge variant={channel.configured ? 'secondary' : 'outline'}>
          {channel.configured
            ? t('settings.notifications.configured')
            : t('settings.notifications.notConfigured')}
        </Badge>
        {channel.configured ? (
          <Badge data-testid={`notification-source-${channel.kind}`} variant="outline">
            {channelSourceLabel(channel)}
          </Badge>
        ) : null}
      </div>
      <div className="min-w-0 text-sm">
        <span className="text-muted-foreground">{t('settings.notifications.host')} </span>
        {channel.host === null ? (
          <span className="text-muted-foreground">{t('settings.notifications.none')}</span>
        ) : (
          <span className="font-mono break-all">{channel.host}</span>
        )}
      </div>
      <div className="min-w-0 text-sm">
        {stored && info.variable === null ? (
          <span className="text-muted-foreground">{t('settings.notifications.storedOnly')}</span>
        ) : stored ? (
          <>
            <span className="text-muted-foreground">
              {t('settings.notifications.storedFromConsole')}{' '}
            </span>
            <code className="break-all">{info.variable}</code>
          </>
        ) : channel.configured && info.variable !== null ? (
          <>
            <span className="text-muted-foreground">{t('settings.notifications.setBy')} </span>
            <code className="break-all">{info.variable}</code>
            {info.fileVariable === undefined ? null : (
              <span className="text-muted-foreground"> {t('settings.notifications.orFile')}</span>
            )}
            {info.aliases.length === 0 ? null : (
              <>
                <span className="text-muted-foreground">
                  {t('settings.notifications.orOlder')}{' '}
                </span>
                {info.aliases.map((alias, index) => (
                  <span key={alias}>
                    {index === 0 ? null : (
                      <span className="text-muted-foreground">
                        {t('settings.notifications.separator')}{' '}
                      </span>
                    )}
                    <code className="break-all">{alias}</code>
                  </span>
                ))}
              </>
            )}
          </>
        ) : (
          <p className="text-muted-foreground leading-relaxed" id={ids.instructions}>
            {channelInstructions(info)}
          </p>
        )}
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <Button
          aria-describedby={[helpId, channel.configured ? null : ids.instructions]
            .filter(Boolean)
            .join(' ')}
          aria-label={t('settings.notifications.sendTestLabel', { channel: channelLabel(info) })}
          disabled={!channel.configured || pending}
          onClick={onTest}
          size="xs"
          type="button"
          variant="outline"
        >
          {pending ? t('settings.notifications.sending') : t('settings.notifications.sendTest')}
        </Button>
        {/* The result is announced when it arrives (role status). */}
        <span
          className={cn(
            'min-w-0 text-sm break-words',
            state?.phase === 'done' && !state.ok ? 'text-destructive' : null,
          )}
          data-testid={`notification-test-result-${channel.kind}`}
          id={ids.result}
          role="status"
        >
          {state?.phase === 'done' ? state.text : ''}
        </span>
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-2 min-[1600px]:col-start-1">
        <label className="text-muted-foreground text-sm" htmlFor={ids.language}>
          {t('settings.notifications.language')}
        </label>
        <NativeSelect
          aria-describedby={languageText === '' ? undefined : ids.languageChange}
          aria-label={t('settings.notifications.languageOf', { channel: channelLabel(info) })}
          className="h-7 w-auto"
          data-testid={`notification-language-${channel.kind}`}
          disabled={languageChange?.phase === 'pending'}
          id={ids.language}
          onChange={(event) =>
            onLanguage(event.target.value === SERVER_LANGUAGE ? null : event.target.value)
          }
          value={channel.language ?? SERVER_LANGUAGE}
        >
          <option value={SERVER_LANGUAGE}>
            {t('settings.notifications.languageServer', {
              language: localeInfo(resolveLocale('server')).name,
            })}
          </option>
          {channelLanguages().map((code) => (
            <option key={code} value={code}>
              {localeInfo(code).name}
            </option>
          ))}
        </NativeSelect>
        <span
          className={cn(
            'min-w-0 text-sm break-words',
            languageChange?.phase === 'done' && !languageChange.ok ? 'text-destructive' : null,
          )}
          data-testid={`notification-language-result-${channel.kind}`}
          id={ids.languageChange}
          role="status"
        >
          {languageText}
        </span>
      </div>
      {/* A group, not a form, and a masked text field, not a password one: a
          password manager offers to save a form's password field, and this URL
          must not end up in one. Enter in the field stores it. */}
      <div
        aria-label={t('settings.notifications.urlFormLabel', { channel: channelLabel(info) })}
        className="flex min-w-0 flex-wrap items-center gap-2 min-[1600px]:col-span-3 min-[1600px]:col-start-2"
        role="group"
      >
        <label className="text-muted-foreground text-sm" htmlFor={ids.url}>
          {addressFieldLabel(info)}
          <span className="sr-only">
            {' '}
            {t('settings.notifications.urlOf', { channel: channelLabel(info) })}
          </span>
        </label>
        <Input
          aria-describedby={[urlHelpId, changeText === '' ? null : ids.change]
            .filter(Boolean)
            .join(' ')}
          aria-invalid={shownProblem === null ? undefined : true}
          autoCapitalize="off"
          autoComplete="off"
          autoCorrect="off"
          className="h-7 w-[26rem] max-w-full [-webkit-text-security:disc]"
          data-1p-ignore
          data-lpignore="true"
          data-testid={`notification-url-${channel.kind}`}
          id={ids.url}
          onChange={(event) => {
            setDraft(event.target.value)
            setProblem(null)
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !changing) {
              event.preventDefault()
              void save()
            }
          }}
          placeholder={placeholder}
          spellCheck={false}
          type="text"
          value={draft}
        />
        <Button
          disabled={changing || liveProblem !== null}
          onClick={() => void save()}
          size="xs"
          type="button"
          variant="outline"
        >
          {stored ? t('settings.notifications.replaceUrl') : t('settings.notifications.setUrl')}
        </Button>
        {stored ? (
          <Button
            aria-label={t('settings.notifications.removeUrlLabel', { channel: channelLabel(info) })}
            disabled={changing}
            onClick={onRemove}
            size="xs"
            type="button"
            variant="outline"
          >
            {t('settings.notifications.removeUrl')}
          </Button>
        ) : null}
        <span
          className={cn('min-w-0 text-sm break-words', changeFailed ? 'text-destructive' : null)}
          data-testid={`notification-change-result-${channel.kind}`}
          id={ids.change}
          role="status"
        >
          {changeText}
        </span>
      </div>
    </li>
  )
}

/**
 * The Notifications tab (11.5a, 11.5b, D18): whether each alert channel is
 * configured, from the server environment or from the console, the host it
 * posts to, the variable that sets it, a test message per channel, and a
 * write-only form that stores a URL in the pglens database (Q2). The webhook
 * address never reaches the browser from the server (I-10).
 */
export function NotificationsPanel() {
  const t = useT()
  const channelsQuery = useNotificationChannels()
  const testChannel = useTestNotificationChannel()
  const setChannel = useSetNotificationChannel()
  const deleteChannel = useDeleteNotificationChannel()
  const setLanguage = useSetNotificationLanguage()
  const [tests, setTests] = useState<Partial<Record<NotificationChannelKind, TestState>>>({})
  const [changes, setChanges] = useState<Partial<Record<NotificationChannelKind, ChangeState>>>({})
  const [languageChanges, setLanguageChanges] = useState<
    Partial<Record<NotificationChannelKind, ChangeState>>
  >({})
  const [removing, setRemoving] = useState<NotificationChannelKind | null>(null)
  const baseId = useId().replaceAll(':', '')
  const helpId = `${baseId}-send-test-help`
  const urlHelpId = `${baseId}-url-help`
  const channels = channelsQuery.data ?? []
  const configured = channels.filter((channel) => channel.configured).length
  const removingInfo = removing === null ? null : notificationChannelInfo(removing)

  async function sendTest(kind: NotificationChannelKind) {
    setTests((current) => ({ ...current, [kind]: { phase: 'pending' } }))
    let next: TestState
    try {
      const result = await testChannel.mutateAsync(kind)
      next = { phase: 'done', ok: result.delivered, text: testResultText(result) }
    } catch (error) {
      next = { phase: 'done', ok: false, text: testFailureMessage(error) }
    }
    setTests((current) => ({ ...current, [kind]: next }))
  }

  async function saveUrl(kind: NotificationChannelKind, url: string): Promise<boolean> {
    setChanges((current) => ({ ...current, [kind]: { phase: 'pending' } }))
    let next: ChangeState
    try {
      await setChannel.mutateAsync({ kind, url })
      next = { phase: 'done', ok: true, text: t('settings.notifications.saved') }
    } catch (error) {
      next = { phase: 'done', ok: false, text: channelChangeFailureMessage(error, 'save') }
    } finally {
      // Drops the settled mutation, whose variables hold the URL, from the
      // mutation cache: gcTime 0 alone keeps it while this observer holds it.
      setChannel.reset()
    }
    setChanges((current) => ({ ...current, [kind]: next }))
    return next.phase === 'done' && next.ok
  }

  async function saveLanguage(kind: NotificationChannelKind, language: string | null) {
    setLanguageChanges((current) => ({ ...current, [kind]: { phase: 'pending' } }))
    let next: ChangeState
    try {
      await setLanguage.mutateAsync({ kind, language })
      next = { phase: 'done', ok: true, text: t('settings.notifications.languageSaved') }
    } catch (error) {
      next = { phase: 'done', ok: false, text: channelChangeFailureMessage(error, 'save') }
    }
    setLanguageChanges((current) => ({ ...current, [kind]: next }))
  }

  async function removeUrl() {
    const kind = removing
    setRemoving(null)
    if (kind === null) return
    setChanges((current) => ({ ...current, [kind]: { phase: 'pending' } }))
    let next: ChangeState
    try {
      const row = await deleteChannel.mutateAsync(kind)
      next = {
        phase: 'done',
        ok: true,
        text: row.configured
          ? t('settings.notifications.removedEnv')
          : t('settings.notifications.removedNone'),
      }
    } catch (error) {
      next = { phase: 'done', ok: false, text: channelChangeFailureMessage(error, 'remove') }
    } finally {
      deleteChannel.reset()
    }
    setChanges((current) => ({ ...current, [kind]: next }))
  }

  return (
    <Section
      actions={
        <HelpLink anchor="settings#notifications" label={t('settings.notifications.title')} />
      }
      captionId="settings.notifications"
      id="settings-notifications"
      title={t('settings.notifications.title')}
    >
      <div className="space-y-4 px-5 py-4">
        <p className="text-muted-foreground text-sm leading-relaxed">
          {t('settings.notifications.intro')} <span id={urlHelpId}>{t(URL_HELP)}</span>{' '}
          <span id={helpId}>{t(SEND_TEST_HELP)}</span>
        </p>
        {channelsQuery.error ? (
          <ErrorState
            endpoint={t('settings.notifications.endpoint')}
            failure={channelsQuery.error}
            onRetry={() => void channelsQuery.refetch()}
          />
        ) : channelsQuery.isPending ? (
          <p aria-busy="true" role="status">
            {t('settings.notifications.loading')}
          </p>
        ) : (
          <>
            <ul aria-label={t('settings.notifications.listLabel')}>
              {channels.map((channel) => (
                <ChannelRow
                  change={changes[channel.kind]}
                  channel={channel}
                  helpId={helpId}
                  key={channel.kind}
                  languageChange={languageChanges[channel.kind]}
                  onLanguage={(language) => void saveLanguage(channel.kind, language)}
                  onRemove={() => setRemoving(channel.kind)}
                  onSave={(url) => saveUrl(channel.kind, url)}
                  onTest={() => void sendTest(channel.kind)}
                  state={tests[channel.kind]}
                  urlHelpId={urlHelpId}
                />
              ))}
            </ul>
            <p className="text-sm" data-testid="notification-channels-summary">
              {configured === 0
                ? t('settings.notifications.noneConfigured')
                : t('settings.notifications.countConfigured', {
                    configured,
                    total: channels.length,
                  })}
            </p>
          </>
        )}
      </div>

      <Dialog
        onOpenChange={(open) => {
          if (!open) setRemoving(null)
        }}
        open={removing !== null}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('settings.notifications.removeTitle', {
                channel:
                  removingInfo === null
                    ? t('settings.notifications.thisChannel')
                    : channelLabel(removingInfo),
              })}
            </DialogTitle>
            <DialogDescription>
              {t('settings.notifications.removeBody', {
                variable: removingInfo?.variable ?? t('settings.notifications.itsVariable'),
              })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                {t('settings.notifications.cancel')}
              </Button>
            </DialogClose>
            <Button onClick={() => void removeUrl()} type="button" variant="destructive">
              {t('settings.notifications.removeUrl')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Section>
  )
}
