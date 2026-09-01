import { useState, type FormEvent } from 'react'

import { useMuteFinding, useUnmuteFinding } from '@/api/findings'
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

type ExpiryPreset = '1h' | '8h' | '24h' | '7d' | 'custom'

const expiryPresets: readonly { value: ExpiryPreset; label: string; seconds?: number }[] = [
  { value: '1h', label: '1 hour', seconds: 60 * 60 },
  { value: '8h', label: '8 hours', seconds: 8 * 60 * 60 },
  { value: '24h', label: '24 hours', seconds: 24 * 60 * 60 },
  { value: '7d', label: '7 days', seconds: 7 * 24 * 60 * 60 },
  { value: 'custom', label: 'Explicit timestamp' },
]

function mutationErrorMessage(error: unknown, action: string): string {
  if (error instanceof Error && error.message) {
    return `Could not ${action}: ${error.message}`
  }
  return `Could not ${action}.`
}

function expiryTimestamp(preset: ExpiryPreset, explicit: string): string | null {
  if (preset === 'custom') {
    const value = new Date(explicit)
    return Number.isFinite(value.getTime()) && value.getTime() > Date.now()
      ? value.toISOString()
      : null
  }

  const seconds = expiryPresets.find((option) => option.value === preset)?.seconds
  return seconds === undefined ? null : new Date(Date.now() + seconds * 1000).toISOString()
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
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [preset, setPreset] = useState<ExpiryPreset>('24h')
  const [explicit, setExplicit] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const mute = useMuteFinding()
  const unmute = useUnmuteFinding()

  const pending = mute.isPending || unmute.isPending

  function openDialog() {
    clearFormState(setReason, setPreset, setExplicit)
    setFormError(null)
    mute.reset()
    setOpen(true)
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const trimmedReason = reason.trim()
    if (!trimmedReason) {
      setFormError('A reason is required before muting a finding.')
      return
    }

    const until = expiryTimestamp(preset, explicit)
    if (!until) {
      setFormError('Choose a future expiry timestamp.')
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
          aria-label="Unmute finding"
          disabled={pending}
          onClick={removeMute}
          type="button"
          variant="outline"
        >
          {unmute.isPending ? 'Removing mute…' : 'Unmute finding'}
        </Button>
      ) : (
        <Button
          aria-label="Mute finding"
          disabled={pending}
          onClick={openDialog}
          type="button"
          variant="outline"
        >
          Mute finding
        </Button>
      )}

      {unmute.error ? (
        <p role="alert">{mutationErrorMessage(unmute.error, 'remove the mute')}</p>
      ) : null}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Mute finding</DialogTitle>
            <DialogDescription>
              Muting does not resolve this finding. When the mute expires, the next evaluation pass
              restores its real state.
            </DialogDescription>
          </DialogHeader>

          <form className="space-y-4" onSubmit={submit}>
            <div className="space-y-2">
              <label className="text-sm font-medium" htmlFor={`mute-reason-${finding.finding_id}`}>
                Reason
              </label>
              <textarea
                aria-describedby={formError ? `mute-error-${finding.finding_id}` : undefined}
                aria-required="true"
                className="border-input bg-background min-h-24 w-full rounded-md border px-3 py-2 text-sm"
                id={`mute-reason-${finding.finding_id}`}
                onChange={(event) => setReason(event.target.value)}
                value={reason}
              />
            </div>

            <fieldset className="space-y-2">
              <legend className="text-sm font-medium">Expiry</legend>
              <div className="grid gap-2 sm:grid-cols-2">
                {expiryPresets.map((option) => (
                  <label className="flex items-center gap-2 text-sm" key={option.value}>
                    <input
                      checked={preset === option.value}
                      name={`mute-expiry-${finding.finding_id}`}
                      onChange={() => setPreset(option.value)}
                      type="radio"
                      value={option.value}
                    />
                    {option.label}
                  </label>
                ))}
              </div>
              <label
                className="flex flex-col gap-1 text-sm"
                htmlFor={`mute-until-${finding.finding_id}`}
              >
                <span>Mute until</span>
                <input
                  aria-label="Mute until"
                  className="border-input bg-background h-9 rounded-md border px-3"
                  disabled={preset !== 'custom'}
                  id={`mute-until-${finding.finding_id}`}
                  onChange={(event) => setExplicit(event.target.value)}
                  type="datetime-local"
                  value={explicit}
                />
              </label>
            </fieldset>

            {formError ? (
              <p id={`mute-error-${finding.finding_id}`} role="alert">
                {formError}
              </p>
            ) : null}
            {mute.error ? (
              <p role="alert">{mutationErrorMessage(mute.error, 'mute the finding')}</p>
            ) : null}
            {mute.isPending ? <p role="status">Sending mute request…</p> : null}

            <DialogFooter>
              <DialogClose asChild>
                <Button disabled={pending} type="button" variant="outline">
                  Cancel
                </Button>
              </DialogClose>
              <Button disabled={pending} type="submit">
                {mute.isPending ? 'Muting…' : 'Mute finding'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}
