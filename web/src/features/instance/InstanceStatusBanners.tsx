import { ReadinessBanner } from '@/features/readiness/ReadinessBanner'

import { CoverageBanner } from './CoverageBanner'

/**
 * The instance dashboards that show the status banners: the instance page,
 * ASH, the query inspector and locks. A query's detail page, the cluster page
 * and the fleet-level pages do not. Case-insensitive, as React Router matches
 * the routes (no route sets caseSensitive).
 */
const INSTANCE_DASHBOARD = /^\/instances\/([^/]+)(?:\/(?:ash|queries|locks))?\/?$/i

/**
 * The instance id of a pathname when it is one of the four instance
 * dashboards, else null. Each page is its own route, with no instance layout
 * around them, so the application shell asks this to decide where the banners
 * go. It lives beside the component on purpose (13.3 contract), at the cost
 * of a react-refresh warning.
 */
export function instanceStatusRoute(pathname: string): string | null {
  const match = INSTANCE_DASHBOARD.exec(pathname)
  if (match === null) return null
  try {
    return decodeURIComponent(match[1]!)
  } catch {
    return null
  }
}

/** Where the shell puts the banners: in the header row from 1600 px, else above the page. */
export type StatusPlacement = 'header' | 'page'

/**
 * The banners of an instance dashboard: data collection after an agent start
 * (13.3). Above the page they stack; in the header row each is one line, so
 * they add no height (D26). The container takes no space while every banner
 * is hidden.
 */
export function InstanceStatusBanners({
  instanceId,
  placement = 'page',
}: {
  instanceId: string
  placement?: StatusPlacement
}) {
  return (
    <div
      className={
        placement === 'header'
          ? 'flex min-w-0 flex-1 items-center gap-2 empty:hidden'
          : 'mb-4 flex flex-col gap-2 empty:hidden min-[1600px]:mb-3'
      }
      data-placement={placement}
      data-testid="instance-status-banners"
    >
      <ReadinessBanner compact={placement === 'header'} instanceId={instanceId} />
      <CoverageBanner compact={placement === 'header'} instanceId={instanceId} />
    </div>
  )
}
