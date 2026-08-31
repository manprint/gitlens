import { useRef, type MouseEvent } from 'react'
import { Outlet } from 'react-router-dom'

import { Header } from './Header'
import { Sidebar } from './Sidebar'

export function AppShell() {
  const mainRef = useRef<HTMLElement>(null)

  function skipToContent(event: MouseEvent<HTMLAnchorElement>) {
    event.preventDefault()
    mainRef.current?.focus()
  }

  return (
    <div className="min-h-screen">
      <a
        className="focus:bg-surface sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:px-3 focus:py-2"
        href="#main"
        onClick={skipToContent}
      >
        Skip to content
      </a>
      <Sidebar />
      <div className="min-h-screen pl-16 lg:pl-64">
        <Header />
        <main ref={mainRef} className="min-h-[calc(100vh-4rem)] p-4" id="main" tabIndex={-1}>
          <Outlet />
        </main>
      </div>
    </div>
  )
}
