import { useId, useRef, useState, type FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useT } from '@/i18n/react'
import {
  NAME_MAX_LENGTH,
  renameErrorMessage,
  trimName,
  validateDisplayName,
  type RenameKind,
} from '@/lib/names'

export interface RenameDialogProps {
  kind: RenameKind
  /** What is renamed, as the console shows it now: the Rename button's name and the description. */
  subject: string
  /** The stored name; null or undefined when there is none. */
  current: string | null | undefined
  /** The label the console shows when the name is cleared. */
  fallback: string
  /** Sends the name (null clears it); settles when the server answered. */
  onSave: (name: string | null) => Promise<unknown>
}

/**
 * The Rename button of one cluster or instance and its dialog (D14, D15). The
 * input starts with the current name; Save stays disabled while the text is
 * invalid, unchanged or being sent; a server error keeps the dialog open with
 * the typed text. Closing returns the focus to the Rename button.
 */
export function RenameDialog({ kind, subject, current, fallback, onSave }: RenameDialogProps) {
  const domId = useId().replaceAll(':', '')
  const inputRef = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState('')
  const [pending, setPending] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)

  const stored = current ? trimName(current) || null : null
  const validation = validateDisplayName(value)
  const unchanged = validation.ok && validation.value === stored
  const inputId = `rename-${domId}`
  const helpId = `rename-help-${domId}`
  const errorId = `rename-error-${domId}`
  const t = useT()
  const kindWord = t(
    kind === 'cluster' ? 'settings.rename.clusterWord' : 'settings.rename.instanceWord',
  )
  const nameLabel = t(
    kind === 'cluster' ? 'settings.rename.clusterName' : 'settings.rename.instanceName',
  )
  const what = t(
    kind === 'cluster' ? 'settings.rename.whatIdentifier' : 'settings.rename.whatAddress',
  )

  function changeOpen(next: boolean) {
    if (pending) return
    if (next) {
      setValue(current ?? '')
      setServerError(null)
    }
    setOpen(next)
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!validation.ok || unchanged || pending) return
    setPending(true)
    setServerError(null)
    try {
      await onSave(validation.value)
      setOpen(false)
    } catch (error) {
      setServerError(renameErrorMessage(error, kind))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogTrigger asChild>
        <Button
          aria-label={t('settings.rename.buttonLabel', { kind: kindWord, subject })}
          size="xs"
          type="button"
          variant="outline"
        >
          {t('settings.rename.button')}
        </Button>
      </DialogTrigger>
      <DialogContent
        onOpenAutoFocus={(event) => {
          event.preventDefault()
          inputRef.current?.focus()
          inputRef.current?.select()
        }}
      >
        <DialogHeader>
          <DialogTitle>{t('settings.rename.title', { kind: kindWord })}</DialogTitle>
          <DialogDescription>
            {t('settings.rename.description', { subject, what })}
          </DialogDescription>
        </DialogHeader>

        <form className="space-y-4" onSubmit={(event) => void submit(event)}>
          <div className="space-y-2">
            <label className="text-sm font-medium" htmlFor={inputId}>
              {nameLabel}
            </label>
            <Input
              aria-describedby={validation.ok ? helpId : `${errorId} ${helpId}`}
              aria-invalid={validation.ok ? undefined : true}
              autoComplete="off"
              id={inputId}
              onChange={(event) => {
                setValue(event.target.value)
                setServerError(null)
              }}
              ref={inputRef}
              value={value}
            />
            {/* Always mounted, so a screen reader hears the message when it appears. */}
            <p aria-live="polite" className="text-destructive text-sm empty:hidden" id={errorId}>
              {validation.ok ? '' : validation.error}
            </p>
            <p className="text-muted-foreground text-xs" id={helpId}>
              {t('settings.rename.help', { max: NAME_MAX_LENGTH, fallback })}
            </p>
          </div>

          {serverError ? (
            <Callout role="alert" variant="destructive">
              {serverError}
            </Callout>
          ) : null}

          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={pending} type="button" variant="outline">
                {t('settings.rename.cancel')}
              </Button>
            </DialogClose>
            <Button disabled={!validation.ok || unchanged || pending} type="submit">
              {pending ? t('settings.rename.saving') : t('settings.rename.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
