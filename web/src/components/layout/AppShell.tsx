import { useRef, useState, type MouseEvent } from 'react'
import { Outlet } from 'react-router-dom'

import { useClusters } from '@/api/queries'
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '@/components/ui/sheet'
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
        />
        <main
          ref={mainRef}
          className="mx-auto min-h-[calc(100vh-3.5rem)] w-full max-w-[1600px] px-4 py-6 outline-none sm:px-6 lg:px-8"
          id="main"
          tabIndex={-1}
        >
          <Outlet />
        </main>
      </div>
    </div>
  )
}
