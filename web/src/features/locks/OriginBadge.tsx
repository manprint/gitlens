import { Badge } from '@/components/ui/badge'
import { CAPTIONS } from '@/lib/captions'
import { sessionOrigin, type Session } from '@/lib/sessions'

/**
 * `app` or `pglens`, from the origin the server set. A session without origin
 * (an older server) shows nothing: the badge never derives the origin itself.
 */
export function OriginBadge({ session }: { session: Session }) {
  const origin = sessionOrigin(session)
  if (origin === null) return null
  return (
    <Badge
      data-caption-id="locks.origin"
      data-origin={origin}
      title={CAPTIONS['locks.origin'].what}
      variant={origin === 'pglens' ? 'outline' : 'secondary'}
    >
      {origin === 'application' ? 'app' : 'pglens'}
    </Badge>
  )
}
