import { CAPTIONS, type CaptionId } from '@/lib/captions'
import { cn } from '@/lib/utils'

/**
 * The one-line explanation of a chart, table or section: what it measures
 * (D30). `oneLine` cuts it to one line from 1600 px, with the sentence in its
 * `title`. Inside a captioned card the card's root carries `data-caption-id`,
 * so the line passes `marked={false}` and each caption is counted once.
 */
export function Caption({
  id,
  className,
  oneLine = false,
  marked = true,
}: {
  id: CaptionId
  className?: string
  oneLine?: boolean
  marked?: boolean
}) {
  const { what } = CAPTIONS[id]
  return (
    <p
      {...(marked ? { 'data-caption-id': id } : {})}
      className={cn(
        'text-muted-foreground text-sm',
        oneLine && 'min-[1600px]:min-w-0 min-[1600px]:truncate',
        className,
      )}
      {...(oneLine ? { title: what } : {})}
    >
      {what}
    </p>
  )
}
