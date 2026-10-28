import { Check, Copy } from 'lucide-react'
import { useState } from 'react'

/** Copies a query's text to the clipboard and confirms it for a moment. */
export function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  function copy() {
    // The clipboard API exists only in secure contexts; elsewhere the button does nothing.
    if (typeof navigator.clipboard?.writeText !== 'function') return
    void navigator.clipboard
      .writeText(text)
      .then(() => {
        setCopied(true)
        window.setTimeout(() => setCopied(false), 1_500)
      })
      .catch(() => undefined)
  }

  return (
    <button
      aria-label={copied ? 'Query text copied' : 'Copy query text'}
      className="text-muted-foreground hover:bg-muted hover:text-foreground inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs font-medium transition-colors"
      onClick={copy}
      type="button"
    >
      {copied ? (
        <Check aria-hidden="true" className="size-3.5" />
      ) : (
        <Copy aria-hidden="true" className="size-3.5" />
      )}
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
}
