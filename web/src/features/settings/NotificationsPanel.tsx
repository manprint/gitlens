import { useId, useState } from 'react'

import {
  useDeleteNotificationChannel,
  useNotificationChannels,
  useSetNotificationChannel,
  useTestNotificationChannel,
} from '@/api/queries'
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
import {
  channelChangeFailureMessage,
  channelInstructions,
  channelSourceLabel,
  notificationChannelInfo,
  testFailureMessage,
  testResultText,
  webhookUrlProblem,
  type NotificationChannel,
  type NotificationChannelKind,
} from '@/lib/notificationChannels'
import { cn } from '@/lib/utils'

/** What Send test does (I-16), referenced by every Send test button. */
const SEND_TEST_HELP =
  'Send test posts the message “pglens test notification” through the channel the alerts use and shows whether it was delivered within 10 s. Nothing is stored.'

/** What the URL form does (I-16), referenced by every URL field. */
const URL_HELP =
  'A URL set here is stored in the pglens database, overrides the server environment and applies to the next alert and test at once. It is write-only: the console never shows it again. It is stored without encryption, so database backups hold it.'

/** The last test send of one channel. */
type TestState = { phase: 'pending' } | { phase: 'done'; ok: boolean; text: string }

/** The last change of one channel's stored URL. */
type ChangeState = { phase: 'pending' } | { phase: 'done'; ok: boolean; text: string }

function ChannelRow({
  channel,
  state,
  change,
  onTest,
  onSave,
  onRemove,
  helpId,
  urlHelpId,
}: {
  channel: NotificationChannel
  state: TestState | undefined
  change: ChangeState | undefined
  onTest: () => void
  /** Stores the URL; resolves true when the server stored it. */
  onSave: (url: string) => Promise<boolean>
  onRemove: () => void
  helpId: string
  urlHelpId: string
}) {
  const info = notificationChannelInfo(channel.kind)
  const id = useId().replaceAll(':', '')
  const ids = {
    title: `${id}-title`,
    instructions: `${id}-instructions`,
    result: `${id}-result`,
    url: `${id}-url`,
    change: `${id}-change`,
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
    const found = webhookUrlProblem(url)
    setProblem(found)
    if (found !== null) return
    if (await onSave(url)) setDraft('')
  }

  const changeText = problem ?? (change?.phase === 'done' ? change.text : changing ? 'Saving…' : '')
  const changeFailed = problem !== null || (change?.phase === 'done' && !change.ok)

  return (
    <li
      aria-labelledby={ids.title}
      className="grid gap-2 border-b py-3 first:pt-0 last:border-b-0 last:pb-0 min-[1600px]:grid-cols-[19rem_14rem_minmax(0,1fr)_minmax(0,22rem)] min-[1600px]:items-center min-[1600px]:gap-x-4"
      data-testid={`notification-channel-${channel.kind}`}
    >
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-foreground text-sm font-medium" id={ids.title}>
          {info.label}
        </h3>
        <Badge variant={channel.configured ? 'secondary' : 'outline'}>
          {channel.configured ? 'configured' : 'not configured'}
        </Badge>
        {channel.configured ? (
          <Badge data-testid={`notification-source-${channel.kind}`} variant="outline">
            {channelSourceLabel(channel)}
          </Badge>
        ) : null}
      </div>
      <div className="min-w-0 text-sm">
        <span className="text-muted-foreground">Host </span>
        {channel.host === null ? (
          <span className="text-muted-foreground">none</span>
        ) : (
          <span className="font-mono break-all">{channel.host}</span>
        )}
      </div>
      <div className="min-w-0 text-sm">
        {stored ? (
          <>
            <span className="text-muted-foreground">Stored from the console; overrides </span>
            <code className="break-all">{info.variable}</code>
          </>
        ) : channel.configured ? (
          <>
            <span className="text-muted-foreground">Set by </span>
            <code className="break-all">{info.variable}</code>
            {info.fileVariable === undefined ? null : (
              <span className="text-muted-foreground"> (or _FILE)</span>
            )}
            {info.aliases.length === 0 ? null : (
              <>
                <span className="text-muted-foreground">, or the older </span>
                {info.aliases.map((alias, index) => (
                  <span key={alias}>
                    {index === 0 ? null : <span className="text-muted-foreground">, </span>}
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
          aria-label={`Send test to ${info.label}`}
          disabled={!channel.configured || pending}
          onClick={onTest}
          size="xs"
          type="button"
          variant="outline"
        >
          {pending ? 'Sending…' : 'Send test'}
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
      {/* A group, not a form, and a masked text field, not a password one: a
          password manager offers to save a form's password field, and this URL
          must not end up in one. Enter in the field stores it. */}
      <div
        aria-label={`URL form of ${info.label}`}
        className="flex min-w-0 flex-wrap items-center gap-2 min-[1600px]:col-span-3 min-[1600px]:col-start-2"
        role="group"
      >
        <label className="text-muted-foreground text-sm" htmlFor={ids.url}>
          Console URL<span className="sr-only"> of {info.label}</span>
        </label>
        <Input
          aria-describedby={[urlHelpId, changeText === '' ? null : ids.change]
            .filter(Boolean)
            .join(' ')}
          aria-invalid={problem === null ? undefined : true}
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
          placeholder={stored ? 'A new URL replaces the stored one' : 'https://…'}
          spellCheck={false}
          type="text"
          value={draft}
        />
        <Button
          disabled={changing}
          onClick={() => void save()}
          size="xs"
          type="button"
          variant="outline"
        >
          {stored ? 'Replace URL' : 'Set URL'}
        </Button>
        {stored ? (
          <Button
            aria-label={`Remove stored URL of ${info.label}`}
            disabled={changing}
            onClick={onRemove}
            size="xs"
            type="button"
            variant="outline"
          >
            Remove stored URL
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
  const channelsQuery = useNotificationChannels()
  const testChannel = useTestNotificationChannel()
  const setChannel = useSetNotificationChannel()
  const deleteChannel = useDeleteNotificationChannel()
  const [tests, setTests] = useState<Partial<Record<NotificationChannelKind, TestState>>>({})
  const [changes, setChanges] = useState<Partial<Record<NotificationChannelKind, ChangeState>>>({})
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
      next = { phase: 'done', ok: true, text: 'Saved. The next alert and test use this URL.' }
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
          ? 'Removed. The channel uses the server environment’s URL again.'
          : 'Removed. The channel is not configured.',
      }
    } catch (error) {
      next = { phase: 'done', ok: false, text: channelChangeFailureMessage(error, 'remove') }
    } finally {
      deleteChannel.reset()
    }
    setChanges((current) => ({ ...current, [kind]: next }))
  }

  return (
    <Section captionId="settings.notifications" id="settings-notifications" title="Notifications">
      <div className="space-y-4 px-5 py-4">
        <p className="text-muted-foreground text-sm leading-relaxed">
          A webhook address comes from the server environment or is stored from the console; the
          console shows its host and never the address. <span id={urlHelpId}>{URL_HELP}</span>{' '}
          <span id={helpId}>{SEND_TEST_HELP}</span>
        </p>
        {channelsQuery.error ? (
          <ErrorState
            endpoint="notification channels"
            failure={channelsQuery.error}
            onRetry={() => void channelsQuery.refetch()}
          />
        ) : channelsQuery.isPending ? (
          <p aria-busy="true" role="status">
            Loading notification channels…
          </p>
        ) : (
          <>
            <ul aria-label="Notification channels">
              {channels.map((channel) => (
                <ChannelRow
                  change={changes[channel.kind]}
                  channel={channel}
                  helpId={helpId}
                  key={channel.kind}
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
                ? 'No channel is configured: alerts are stored but not delivered.'
                : `${configured} of ${channels.length} channels configured.`}
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
              Remove the stored URL of {removingInfo?.label ?? 'this channel'}?
            </DialogTitle>
            <DialogDescription>
              The URL stored from the console is deleted from the pglens database. The channel falls
              back to the server environment’s URL ({removingInfo?.variable ?? 'its variable'}) if
              one is set; without one it is not configured and alerts are not delivered to it.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button onClick={() => void removeUrl()} type="button" variant="destructive">
              Remove stored URL
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Section>
  )
}
