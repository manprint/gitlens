import { displayTimeZone, fromZonedInput, toZonedInput } from '@/lib/timezone'
import { useMemo, useState } from 'react'

import { useCreateSilence } from '@/api/silences'
import { HelpLink } from '@/components/layout/HelpLink'
import { Input } from '@/components/ui/input'
import type { Alert, Silence } from '@/lib/alerts'
import { matchSilence } from '@/lib/alerts'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { useT } from '@/i18n/react'

interface MatcherDraft {
  name: string
  value: string
}

interface SilenceEditorProps {
  firingAlerts: Alert[]
  previewUnavailable?: boolean
  onCancel: () => void
  onCreated: () => void
}

// A datetime-local input holds wall-clock text; both directions read it in the
// console's zone, so the default window and the saved one never shift by the offset.
function dateTimeLocal(timestamp: number): string {
  return toZonedInput(timestamp, displayTimeZone())
}

function isoTimestamp(value: string): string | null {
  return fromZonedInput(value, displayTimeZone())?.toISOString() ?? null
}

const emptyMatcher: MatcherDraft = { name: '', value: '' }

export function SilenceEditor({
  firingAlerts,
  previewUnavailable = false,
  onCancel,
  onCreated,
}: SilenceEditorProps) {
  const t = useT()
  const [matchers, setMatchers] = useState<MatcherDraft[]>([{ ...emptyMatcher }])
  const [reason, setReason] = useState('')
  const [startsAt, setStartsAt] = useState(() => dateTimeLocal(Date.now()))
  const [endsAt, setEndsAt] = useState(() => dateTimeLocal(Date.now() + 60 * 60 * 1000))
  const [confirmAll, setConfirmAll] = useState(false)
  const [validationError, setValidationError] = useState<string | null>(null)
  const createSilence = useCreateSilence()

  const completeMatchers = useMemo(
    () =>
      matchers
        .filter((matcher) => matcher.name.trim() || matcher.value.trim())
        .map((matcher) => ({ name: matcher.name.trim(), value: matcher.value.trim() })),
    [matchers],
  )

  const previewAlerts = useMemo(() => {
    if (
      completeMatchers.length === 0 ||
      completeMatchers.some((matcher) => !matcher.name || !matcher.value)
    ) {
      return []
    }
    const previewSilence = {
      ends_at: endsAt,
      matchers: completeMatchers,
      reason,
      silence_id: '00000000-0000-4000-8000-000000000000',
      starts_at: startsAt,
    } as Silence
    return firingAlerts.filter((alert) => matchSilence(alert, previewSilence))
  }, [completeMatchers, endsAt, firingAlerts, reason, startsAt])

  const matchesAllFiring =
    !previewUnavailable && firingAlerts.length > 0 && previewAlerts.length === firingAlerts.length

  function updateMatcher(index: number, key: keyof MatcherDraft, value: string) {
    setMatchers((current) =>
      current.map((matcher, matcherIndex) =>
        matcherIndex === index ? { ...matcher, [key]: value } : matcher,
      ),
    )
  }

  function applyPreset(durationHours: number) {
    // eslint-disable-next-line react-hooks/purity -- preset values intentionally follow the current clock.
    const presetNow = Date.now()
    setStartsAt(dateTimeLocal(presetNow))
    setEndsAt(dateTimeLocal(presetNow + durationHours * 60 * 60 * 1000))
  }

  function submit() {
    const hasPartialMatcher = matchers.some(
      (matcher) => Boolean(matcher.name.trim()) !== Boolean(matcher.value.trim()),
    )
    if (hasPartialMatcher || completeMatchers.length === 0) {
      setValidationError(t('alerts.silenceEditor.matcherIncomplete'))
      return
    }
    if (!reason.trim()) {
      setValidationError(t('alerts.silenceEditor.reasonRequired'))
      return
    }
    const start = isoTimestamp(startsAt)
    const end = isoTimestamp(endsAt)
    if (!start || !end) {
      setValidationError(t('alerts.silenceEditor.datesInvalid'))
      return
    }
    if (new Date(end).getTime() <= new Date(start).getTime()) {
      setValidationError(t('alerts.silenceEditor.endBeforeStart'))
      return
    }
    if (matchesAllFiring && !confirmAll) {
      setValidationError(t('alerts.silenceEditor.confirmAll', { count: firingAlerts.length }))
      return
    }

    setValidationError(null)
    createSilence.mutate(
      {
        ends_at: end,
        matchers: completeMatchers,
        reason: reason.trim(),
        starts_at: start,
      },
      { onSuccess: onCreated },
    )
  }

  return (
    <section aria-labelledby="silence-editor-title" className="border-primary/30 bg-primary/5 p-4">
      <div className="mb-4 flex items-start justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 id="silence-editor-title" className="text-lg font-semibold">
              {t('alerts.silenceEditor.title')}
            </h2>
            <HelpLink anchor="silences#create" label={t('alerts.silenceEditor.title')} />
          </div>
          <p className="text-muted-foreground text-sm">{t('alerts.silenceEditor.intro')}</p>
        </div>
        <Button size="sm" variant="outline" onClick={onCancel} type="button">
          {t('alerts.silenceEditor.cancel')}
        </Button>
      </div>

      <form
        className="space-y-5"
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          submit()
        }}
      >
        <fieldset className="space-y-3">
          <legend className="font-medium">{t('alerts.silences.matchers')}</legend>
          <p className="text-muted-foreground text-xs" id="silence-matchers-help">
            {t('alerts.silenceEditor.matchersHelp')}
          </p>
          {matchers.map((matcher, index) => (
            <div className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]" key={index}>
              <label>
                <span className="text-sm">
                  {t('alerts.silenceEditor.matcherName', { index: index + 1 })}
                </span>
                <Input
                  aria-label={t('alerts.silenceEditor.matcherName', { index: index + 1 })}
                  onChange={(event) => updateMatcher(index, 'name', event.target.value)}
                  value={matcher.name}
                />
              </label>
              <label>
                <span className="text-sm">
                  {t('alerts.silenceEditor.matcherValue', { index: index + 1 })}
                </span>
                <Input
                  aria-label={t('alerts.silenceEditor.matcherValue', { index: index + 1 })}
                  onChange={(event) => updateMatcher(index, 'value', event.target.value)}
                  value={matcher.value}
                />
              </label>
              {matchers.length > 1 ? (
                <Button
                  size="sm"
                  variant="outline"
                  aria-label={t('alerts.silenceEditor.removeMatcher', { index: index + 1 })}
                  onClick={() =>
                    setMatchers((current) =>
                      current.filter((_, matcherIndex) => matcherIndex !== index),
                    )
                  }
                  type="button"
                >
                  {t('alerts.silenceEditor.remove')}
                </Button>
              ) : null}
            </div>
          ))}
          <Button
            size="sm"
            variant="outline"
            onClick={() => setMatchers((current) => [...current, { ...emptyMatcher }])}
            type="button"
          >
            {t('alerts.silenceEditor.addMatcher')}
          </Button>
        </fieldset>

        <label className="block">
          <span className="text-sm">{t('alerts.silences.reason')}</span>
          <Input
            aria-label={t('alerts.silences.reason')}
            aria-describedby="silence-reason-help"
            aria-invalid={validationError === t('alerts.silenceEditor.reasonRequired')}
            onChange={(event) => setReason(event.target.value)}
            value={reason}
          />
          <span className="text-muted-foreground text-xs" id="silence-reason-help">
            {t('alerts.silenceEditor.reasonHelp')}
          </span>
        </label>

        <div className="grid gap-4 sm:grid-cols-2">
          <label>
            <span className="text-sm">{t('alerts.silences.start')}</span>
            <Input
              aria-describedby="silence-window-help"
              aria-label={t('alerts.silences.start')}
              onChange={(event) => setStartsAt(event.target.value)}
              type="datetime-local"
              value={startsAt}
            />
          </label>
          <label>
            <span className="text-sm">{t('alerts.silences.end')}</span>
            <Input
              aria-describedby="silence-window-help"
              aria-label={t('alerts.silences.end')}
              onChange={(event) => setEndsAt(event.target.value)}
              type="datetime-local"
              value={endsAt}
            />
          </label>
        </div>
        <p className="text-muted-foreground text-xs" id="silence-window-help">
          {t('alerts.silenceEditor.windowHelp', { zone: displayTimeZone() })}
        </p>
        <div
          aria-label={t('alerts.silenceEditor.presetsLabel')}
          className="flex flex-wrap gap-2"
          role="group"
        >
          <span className="text-sm">{t('alerts.silenceEditor.presets')}</span>
          {[1, 4, 24].map((hours) => (
            <Button
              size="sm"
              variant="outline"
              key={hours}
              onClick={() => applyPreset(hours)}
              type="button"
            >
              {t('alerts.silenceEditor.hours', { count: hours })}
            </Button>
          ))}
        </div>

        <div aria-live="polite" className="bg-muted/20 rounded-lg border p-3 text-sm">
          <strong>{t('alerts.silenceEditor.preview')}</strong>
          {previewUnavailable ? (
            <p>{t('alerts.silenceEditor.previewUnavailable')}</p>
          ) : completeMatchers.length === 0 ||
            completeMatchers.some((matcher) => !matcher.name || !matcher.value) ? (
            <p>{t('alerts.silenceEditor.previewIncomplete')}</p>
          ) : previewAlerts.length > 0 ? (
            <>
              <p>{t('alerts.silenceEditor.wouldSuppress', { count: previewAlerts.length })}</p>
              <ul className="list-disc pl-5">
                {previewAlerts.map((alert) => (
                  <li key={alert.alert_key}>{alert.alert_key}</li>
                ))}
              </ul>
            </>
          ) : (
            <p>{t('alerts.silenceEditor.noMatch')}</p>
          )}
        </div>

        {matchesAllFiring ? (
          <label className="border-destructive/40 bg-destructive/10 block rounded-lg border p-3 text-sm">
            <span>{t('alerts.silenceEditor.confirmBlock', { count: firingAlerts.length })}</span>
            <span className="mt-2 flex items-center gap-2">
              <input
                aria-label={t('alerts.silenceEditor.understand', { count: firingAlerts.length })}
                checked={confirmAll}
                onChange={(event) => setConfirmAll(event.target.checked)}
                type="checkbox"
              />
              {t('alerts.silenceEditor.understand', { count: firingAlerts.length })}
            </span>
          </label>
        ) : null}

        {validationError ? <Callout variant="destructive">{validationError}</Callout> : null}
        {createSilence.error ? (
          <Callout variant="destructive">{createSilence.error.message}</Callout>
        ) : null}
        <div className="flex gap-2">
          <Button size="sm" disabled={createSilence.isPending} type="submit">
            {createSilence.isPending
              ? t('alerts.silenceEditor.creating')
              : t('alerts.silenceEditor.submit')}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={createSilence.isPending}
            onClick={onCancel}
            type="button"
          >
            {t('alerts.silenceEditor.cancel')}
          </Button>
        </div>
      </form>
    </section>
  )
}
