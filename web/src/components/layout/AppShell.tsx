import { useLayoutEffect, useRef, useState, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'
import { Outlet, useLocation } from 'react-router-dom'

import { useClusters } from '@/api/queries'
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '@/components/ui/sheet'
import {
  InstanceStatusBanners,
  instanceStatusRoute,
} from '@/features/instance/InstanceStatusBanners'
import { useMinWidth } from '@/hooks/useMinWidth'
import { Header } from './Header'
import { Sidebar, SidebarContent } from './Sidebar'

export function AppShell() {
  const mainRef = useRef<HTMLElement>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  // The header reports reachability and data age wherever the operator is. The
  // fleet, cluster, instance and settings pages already poll this query, so they
  // share it. On the other pages the shell adds its own poll of GET
  // /api/v1/clusters every 15 seconds. The sidebar reads the same answer for
  // its server switcher.
  const fleet = useClusters()
  const connection = {
    lastPollFailed: fleet.isError,
    lastSuccessAt: fleet.dataUpdatedAt > 0 ? fleet.dataUpdatedAt : null,
  }
  const clusters = fleet.data ?? []
  // The instance dashboards share the status banners (13.3). Each page is its
  // own route, so the shell is their one mount point. From 1600 px they sit in
  // the header row, where they add no height to a page that must fit a Full HD
  // screen (D26); below it, above the page. One copy is rendered, into a node
  // of its own that moves between the two places: crossing 1600 px neither
  // remounts the banners nor asks for their data again, and there is one live
  // region per banner.
  const statusInstance = instanceStatusRoute(useLocation().pathname)
  const statusInHeader = useMinWidth(1600)
  // `display: contents`: the host adds no box, so each place lays the
  // banners out as its own child.
  const [statusHost] = useState(() => {
    const host = document.createElement('div')
    host.className = 'contents'
    return host
  })
  const [headerSlot, setHeaderSlot] = useState<HTMLDivElement | null>(null)
  const [pageSlot, setPageSlot] = useState<HTMLDivElement | null>(null)
  useLayoutEffect(() => {
    const target = statusInHeader ? headerSlot : pageSlot
    if (target === null) return
    target.appendChild(statusHost)
    return () => statusHost.remove()
  }, [headerSlot, pageSlot, statusHost, statusInHeader])

  function skipToContent(event: MouseEvent<HTMLAnchorElement>) {
    event.preventDefault()
    mainRef.current?.focus()
  }

  return (
    <div className="bg-background min-h-screen">
      <a
        className="focus:bg-card sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:px-3 focus:py-2 focus:shadow-lg"
        href="#main"
        onClick={skipToContent}
      >
        Skip to content
      </a>
      <Sidebar clusters={clusters} />
      <Sheet onOpenChange={setDrawerOpen} open={drawerOpen}>
        <SheetContent
          className="bg-sidebar text-sidebar-foreground w-72 border-none p-0"
          side="left"
        >
          <SheetTitle className="sr-only">Navigation</SheetTitle>
          <SheetDescription className="sr-only">
            Choose a server or a page to open.
          </SheetDescription>
          <SidebarContent clusters={clusters} expanded onNavigate={() => setDrawerOpen(false)} />
        </SheetContent>
      </Sheet>
      <div className="min-h-screen md:pl-16 lg:pl-64">
        <Header
          connection={connection}
          dataUpdatedAt={fleet.dataUpdatedAt}
          onOpenNavigation={() => setDrawerOpen(true)}
          statusSlot={statusInHeader && statusInstance !== null ? setHeaderSlot : undefined}
        />
        <main
          ref={mainRef}
          // The header is 3.5rem plus its 1px border; from 1600 px the content takes the full
          // width with a tighter vertical padding (D26).
          className="mx-auto min-h-[calc(100vh-3.5rem-1px)] w-full max-w-[1600px] px-4 py-6 outline-none min-[1600px]:max-w-none min-[1600px]:py-4 sm:px-6 lg:px-8"
          id="main"
          tabIndex={-1}
        >
          {!statusInHeader && statusInstance !== null ? <div ref={setPageSlot} /> : null}
          <Outlet />
        </main>
      </div>
      {statusInstance !== null
        ? createPortal(
            <InstanceStatusBanners
              key={statusInstance}
              instanceId={statusInstance}
              placement={statusInHeader ? 'header' : 'page'}
            />,
            statusHost,
          )
        : null}
    </div>
  )
}
