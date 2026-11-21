import { displayTimeZone, fromZonedInput, toZonedInput } from '@/lib/timezone'
import { useId, useState, type FormEvent } from 'react'

import { useMuteFinding, useUnmuteFinding } from '@/api/findings'
import { HelpLink } from '@/components/layout/HelpLink'
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
import type { JoinedFinding } from '@/lib/findings'
import { Input } from '@/components/ui/input'
import { Callout } from '@/components/ui/callout'
import { useT, type Translate } from '@/i18n/react'

type ExpiryPreset = '1h' | '8h' | '24h' | '7d' | 'custom'

const expiryPresets: readonly { value: ExpiryPreset; seconds?: number }[] = [
  { value: '1h', seconds: 60 * 60 },
  { value: '8h', seconds: 8 * 60 * 60 },
  { value: '24h', seconds: 24 * 60 * 60 },
  { value: '7d', seconds: 7 * 24 * 60 * 60 },
  { value: 'custom' },
]

function presetLabel(t: Translate, preset: ExpiryPreset): string {
  if (preset === '1h') return t('findings.mute.preset1h')
  if (preset === '8h') return t('findings.mute.preset8h')
  if (preset === '24h') return t('findings.mute.preset24h')
  if (preset === '7d') return t('findings.mute.preset7d')
  return t('findings.mute.presetCustom')
}

function mutationErrorMessage(t: Translate, error: unknown, action: string): string {
  if (error instanceof Error && error.message) {
    return t('findings.mute.errorDetail', { action, detail: error.message })
  }
  return t('findings.mute.error', { action })
}

function expiryTimestamp(preset: ExpiryPreset, explicit: string): string | null {
  if (preset === 'custom') {
    const value = fromZonedInput(explicit, displayTimeZone())
    return value !== null && value.getTime() > Date.now() ? value.toISOString() : null
  }

  const seconds = expiryPresets.find((option) => option.value === preset)?.seconds
  return seconds === undefined ? null : new Date(Date.now() + seconds * 1000).toISOString()
}

// The server refuses a mute that runs past this many days from its own clock.
const maxMuteDays = 90

// The wall clock in the console's zone as a datetime-local value, down to the minute.
function localDateTimeValue(time: Date): string {
  return toZonedInput(time.getTime(), displayTimeZone())
}

function clearFormState(
  setReason: (value: string) => void,
  setPreset: (value: ExpiryPreset) => void,
  setExplicit: (value: string) => void,
) {
  setReason('')
  setPreset('24h')
  setExplicit('')
}

export function MuteDialog({ finding }: { finding: JoinedFinding }) {
  const t = useT()
  // A finding id holds its database and object names, which can contain
  // spaces: as an element id it split the aria-describedby reference.
  const domId = useId().replaceAll(':', '')
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [preset, setPreset] = useState<ExpiryPreset>('24h')
  const [explicit, setExplicit] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const [maxUntil, setMaxUntil] = useState('')
  const mute = useMuteFinding()
  const unmute = useUnmuteFinding()

  const pending = mute.isPending || unmute.isPending

  function openDialog() {
    clearFormState(setReason, setPreset, setExplicit)
    setFormError(null)
    setMaxUntil(localDateTimeValue(new Date(Date.now() + maxMuteDays * 24 * 60 * 60 * 1000)))
    mute.reset()
    setOpen(true)
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const trimmedReason = reason.trim()
    if (!trimmedReason) {
      setFormError(t('findings.mute.reasonRequired'))
      return
    }

    const until = expiryTimestamp(preset, explicit)
    if (!until) {
      setFormError(t('findings.mute.expiryRequired'))
      return
    }

    setFormError(null)
    mute.mutate(
      { findingId: finding.finding_id, reason: trimmedReason, until },
      { onSuccess: () => setOpen(false) },
    )
  }

  function removeMute() {
    unmute.reset()
    unmute.mutate({ findingId: finding.finding_id })
  }

  return (
    <>
      {finding.state === 'muted' ? (
        <Button
          aria-label={t('findings.mute.unmute')}
          disabled={pending}
          onClick={removeMute}
          type="button"
          variant="outline"
        >
          {unmute.isPending ? t('findings.mute.removing') : t('findings.mute.unmute')}
        </Button>
      ) : (
        <Button
          aria-label={t('findings.mute.mute')}
          disabled={pending}
          onClick={openDialog}
          type="button"
          variant="outline"
        >
          {t('findings.mute.mute')}
        </Button>
      )}

      {unmute.error ? (
        <Callout variant="destructive">
          {mutationErrorMessage(t, unmute.error, t('findings.mute.actionRemove'))}
        </Callout>
      ) : null}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <div className="flex flex-wrap items-center gap-2">
              <DialogTitle>{t('findings.mute.mute')}</DialogTitle>
              <HelpLink anchor="findings#mute-dialog" label={t('findings.mute.mute')} />
            </div>
            <DialogDescription>{t('findings.mute.description')}</DialogDescription>
          </DialogHeader>

          <form className="space-y-4" onSubmit={submit}>
            <div className="space-y-2">
              <label className="text-sm font-medium" htmlFor={`mute-reason-${domId}`}>
                {t('findings.mute.reason')}
              </label>
              <textarea
                aria-describedby={formError ? `mute-error-${domId}` : undefined}
                aria-required="true"
                className="border-input bg-background min-h-24 w-full rounded-md border px-3 py-2 text-sm"
                id={`mute-reason-${domId}`}
                onChange={(event) => setReason(event.target.value)}
                value={reason}
              />
            </div>

            <fieldset className="space-y-2">
              <legend className="text-sm font-medium">{t('findings.mute.expiry')}</legend>
              <div className="grid gap-2 sm:grid-cols-2">
                {expiryPresets.map((option) => (
                  <label className="flex items-center gap-2 text-sm" key={option.value}>
                    <input
                      checked={preset === option.value}
                      name={`mute-expiry-${domId}`}
                      onChange={() => setPreset(option.value)}
                      type="radio"
                      value={option.value}
                    />
                    {presetLabel(t, option.value)}
                  </label>
                ))}
              </div>
              <label className="flex flex-col gap-1 text-sm" htmlFor={`mute-until-${domId}`}>
                <span>{t('findings.mute.until')}</span>
                <Input
                  aria-label={t('findings.mute.until')}
                  className="w-auto"
                  disabled={preset !== 'custom'}
                  id={`mute-until-${domId}`}
                  max={maxUntil || undefined}
                  onChange={(event) => setExplicit(event.target.value)}
                  type="datetime-local"
                  value={explicit}
                />
              </label>
            </fieldset>

            {formError ? (
              <p className="text-destructive text-sm" id={`mute-error-${domId}`} role="alert">
                {formError}
              </p>
            ) : null}
            {mute.error ? (
              <Callout variant="destructive">
                {mutationErrorMessage(t, mute.error, t('findings.mute.actionMute'))}
              </Callout>
            ) : null}
            {mute.isPending ? <p role="status">{t('findings.mute.sending')}</p> : null}

            <DialogFooter>
              <DialogClose asChild>
                <Button disabled={pending} type="button" variant="outline">
                  {t('findings.mute.cancel')}
                </Button>
              </DialogClose>
              <Button disabled={pending} type="submit">
                {mute.isPending ? t('findings.mute.muting') : t('findings.mute.mute')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}
