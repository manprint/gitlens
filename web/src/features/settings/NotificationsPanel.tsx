import { useId, useState } from 'react'

import { useNotificationChannels, useTestNotificationChannel } from '@/api/queries'
import { Section } from '@/components/layout/Section'
import { ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  channelInstructions,
  notificationChannelInfo,
  testFailureMessage,
  testResultText,
  type NotificationChannel,
  type NotificationChannelKind,
} from '@/lib/notificationChannels'
import { cn } from '@/lib/utils'

/** What Send test does (I-16), referenced by every Send test button. */
const SEND_TEST_HELP =
  'Send test posts the message “pglens test notification” through the channel the alerts use and shows whether it was delivered within 10 s. Nothing is stored.'

/** The last test send of one channel. */
type TestState = { phase: 'pending' } | { phase: 'done'; ok: boolean; text: string }

function ChannelRow({
  channel,
  state,
  onTest,
  helpId,
}: {
  channel: NotificationChannel
  state: TestState | undefined
  onTest: () => void
  helpId: string
}) {
  const info = notificationChannelInfo(channel.kind)
  const id = useId().replaceAll(':', '')
  const ids = { title: `${id}-title`, instructions: `${id}-instructions`, result: `${id}-result` }
  const pending = state?.phase === 'pending'

  return (
    <li
      aria-labelledby={ids.title}
      className="grid gap-2 border-b py-3 first:pt-0 last:border-b-0 last:pb-0 min-[1600px]:grid-cols-[14rem_16rem_minmax(0,1fr)_minmax(0,22rem)] min-[1600px]:items-center min-[1600px]:gap-4"
      data-testid={`notification-channel-${channel.kind}`}
    >
      <div className="flex items-center gap-2">
        <h3 className="text-foreground text-sm font-medium" id={ids.title}>
          {info.label}
        </h3>
        <Badge variant={channel.configured ? 'secondary' : 'outline'}>
          {channel.configured ? 'configured' : 'not configured'}
        </Badge>
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
        {channel.configured ? (
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
    </li>
  )
}

/**
 * The Notifications tab (11.5a, D18): whether the server environment
 * configures each alert channel, the host it posts to, the variable that sets
 * it, and a test message per channel. The webhook address never reaches the
 * browser (I-10); storing one from the console waits on Q2 (11.5b).
 */
export function NotificationsPanel() {
  const channelsQuery = useNotificationChannels()
  const testChannel = useTestNotificationChannel()
  const [tests, setTests] = useState<Partial<Record<NotificationChannelKind, TestState>>>({})
  const helpId = `${useId().replaceAll(':', '')}-send-test-help`
  const channels = channelsQuery.data ?? []
  const configured = channels.filter((channel) => channel.configured).length

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

  return (
    <Section captionId="settings.notifications" id="settings-notifications" title="Notifications">
      <div className="space-y-4 px-5 py-4">
        <p className="text-muted-foreground text-sm leading-relaxed">
          Webhook addresses are set in the server environment only; the console shows the host and
          never the address. <span id={helpId}>{SEND_TEST_HELP}</span>
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
                  channel={channel}
                  helpId={helpId}
                  key={channel.kind}
                  onTest={() => void sendTest(channel.kind)}
                  state={tests[channel.kind]}
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
    </Section>
  )
}
