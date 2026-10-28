import { Check, Copy, X } from 'lucide-react'
import { useEffect, useState } from 'react'

type CopyState = 'idle' | 'copied' | 'failed'

/**
 * The clipboard API exists only in secure contexts, and pglens is often served
 * over plain HTTP on a private network. There the text is copied through a
 * hidden text area, the way browsers copied before the API existed.
 */
function copyWithSelection(text: string): boolean {
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.append(area)
  area.select()
  try {
    // execCommand is deprecated, but it is the only copy path outside secure contexts.
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    area.remove()
  }
}

async function copyText(text: string): Promise<boolean> {
  if (typeof navigator.clipboard?.writeText === 'function') {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // A denied permission falls through to the selection copy.
    }
  }
  return copyWithSelection(text)
}

/** Copies a query's text to the clipboard and confirms it, or the failure, for a moment. */
export function CopyButton({ text }: { text: string }) {
  const [state, setState] = useState<CopyState>('idle')

  useEffect(() => {
    if (state === 'idle') return
    const timer = window.setTimeout(() => setState('idle'), 1_500)
    return () => window.clearTimeout(timer)
  }, [state])

  function copy() {
    void copyText(text).then((copied) => setState(copied ? 'copied' : 'failed'))
  }

  const Icon = state === 'copied' ? Check : state === 'failed' ? X : Copy
  return (
    <button
      aria-label={
        state === 'copied'
          ? 'Query text copied'
          : state === 'failed'
            ? 'Could not copy query text'
            : 'Copy query text'
      }
      className="text-muted-foreground hover:bg-muted hover:text-foreground inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs font-medium transition-colors"
      onClick={copy}
      type="button"
    >
      <Icon aria-hidden="true" className="size-3.5" />
      {state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : 'Copy'}
    </button>
  )
}
