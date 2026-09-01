import { useMemo, useState } from 'react'

import { useCreateSilence } from '@/api/silences'
import { Input } from '@/components/ui/input'
import type { Alert, Silence } from '@/lib/alerts'
import { matchSilence } from '@/lib/alerts'

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

function dateTimeLocal(timestamp: number): string {
  return new Date(timestamp).toISOString().slice(0, 16)
}

function isoTimestamp(value: string): string | null {
  const timestamp = new Date(value).getTime()
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : null
}

const emptyMatcher: MatcherDraft = { name: '', value: '' }

export function SilenceEditor({
  firingAlerts,
  previewUnavailable = false,
  onCancel,
  onCreated,
}: SilenceEditorProps) {
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
      setValidationError('Each matcher needs both a name and a value.')
      return
    }
    if (!reason.trim()) {
      setValidationError('Reason is required.')
      return
    }
    const start = isoTimestamp(startsAt)
    const end = isoTimestamp(endsAt)
    if (!start || !end) {
      setValidationError('Start and end must be valid dates.')
      return
    }
    if (new Date(end).getTime() <= new Date(start).getTime()) {
      setValidationError('End must be after start.')
      return
    }
    if (matchesAllFiring && !confirmAll) {
      setValidationError(
        `Confirm that this silence will suppress all ${firingAlerts.length} currently firing alerts.`,
      )
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
          <h2 id="silence-editor-title" className="text-lg font-semibold">
            Create silence
          </h2>
          <p className="text-muted-foreground text-sm">
            A silence suppresses notifications only; the alert remains visible and continues to be
            evaluated.
          </p>
        </div>
        <button onClick={onCancel} type="button">
          Cancel
        </button>
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
          <legend className="font-medium">Matchers</legend>
          {matchers.map((matcher, index) => (
            <div className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]" key={index}>
              <label>
                <span className="text-sm">Matcher {index + 1} name</span>
                <Input
                  aria-label={`Matcher ${index + 1} name`}
                  onChange={(event) => updateMatcher(index, 'name', event.target.value)}
                  value={matcher.name}
                />
              </label>
              <label>
                <span className="text-sm">Matcher {index + 1} value</span>
                <Input
                  aria-label={`Matcher ${index + 1} value`}
                  onChange={(event) => updateMatcher(index, 'value', event.target.value)}
                  value={matcher.value}
                />
              </label>
              {matchers.length > 1 ? (
                <button
                  aria-label={`Remove matcher ${index + 1}`}
                  onClick={() =>
                    setMatchers((current) =>
                      current.filter((_, matcherIndex) => matcherIndex !== index),
                    )
                  }
                  type="button"
                >
                  Remove
                </button>
              ) : null}
            </div>
          ))}
          <button
            onClick={() => setMatchers((current) => [...current, { ...emptyMatcher }])}
            type="button"
          >
            Add matcher
          </button>
        </fieldset>

        <label className="block">
          <span className="text-sm">Reason</span>
          <Input
            aria-label="Reason"
            aria-describedby="silence-reason-help"
            aria-invalid={validationError === 'Reason is required.'}
            onChange={(event) => setReason(event.target.value)}
            value={reason}
          />
          <span className="text-muted-foreground text-xs" id="silence-reason-help">
            Explain why notifications should be suppressed.
          </span>
        </label>

        <div className="grid gap-4 sm:grid-cols-2">
          <label>
            <span className="text-sm">Start</span>
            <Input
              aria-label="Start"
              onChange={(event) => setStartsAt(event.target.value)}
              type="datetime-local"
              value={startsAt}
            />
          </label>
          <label>
            <span className="text-sm">End</span>
            <Input
              aria-label="End"
              onChange={(event) => setEndsAt(event.target.value)}
              type="datetime-local"
              value={endsAt}
            />
          </label>
        </div>
        <div aria-label="Silence duration presets" className="flex flex-wrap gap-2" role="group">
          <span className="text-sm">Presets:</span>
          {[1, 4, 24].map((hours) => (
            <button key={hours} onClick={() => applyPreset(hours)} type="button">
              {hours} hour{hours === 1 ? '' : 's'}
            </button>
          ))}
        </div>

        <div aria-live="polite" className="border-muted bg-muted/20 p-3 text-sm">
          <strong>Live preview</strong>
          {previewUnavailable ? (
            <p>Current firing alerts are unavailable, so the preview cannot be verified.</p>
          ) : completeMatchers.length === 0 ||
            completeMatchers.some((matcher) => !matcher.name || !matcher.value) ? (
            <p>Complete a matcher to see which currently firing alerts would be silenced.</p>
          ) : previewAlerts.length > 0 ? (
            <>
              <p>
                Would suppress notifications for {previewAlerts.length} currently firing alert(s):
              </p>
              <ul className="list-disc pl-5">
                {previewAlerts.map((alert) => (
                  <li key={alert.alert_key}>{alert.alert_key}</li>
                ))}
              </ul>
            </>
          ) : (
            <p>No currently firing alerts match these matchers.</p>
          )}
        </div>

        {matchesAllFiring ? (
          <label className="border-destructive/40 bg-destructive/10 block p-3 text-sm">
            <span>
              This silence would suppress notifications for all {firingAlerts.length} currently
              firing alerts. Confirm to continue.
            </span>
            <span className="mt-2 flex items-center gap-2">
              <input
                aria-label={`I understand this will suppress notifications for all ${firingAlerts.length} currently firing alerts`}
                checked={confirmAll}
                onChange={(event) => setConfirmAll(event.target.checked)}
                type="checkbox"
              />
              I understand this will suppress notifications for all {firingAlerts.length} currently
              firing alerts
            </span>
          </label>
        ) : null}

        {validationError ? <p role="alert">{validationError}</p> : null}
        {createSilence.error ? <p role="alert">{createSilence.error.message}</p> : null}
        <div className="flex gap-2">
          <button disabled={createSilence.isPending} type="submit">
            {createSilence.isPending ? 'Creating…' : 'Create silence'}
          </button>
          <button disabled={createSilence.isPending} onClick={onCancel} type="button">
            Cancel
          </button>
        </div>
      </form>
    </section>
  )
}
