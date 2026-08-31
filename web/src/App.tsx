import { Outlet } from 'react-router-dom'

export default function App() {
  return (
    <main data-testid="application-shell">
      <header>
        <p>pglens web interface</p>
        <small>Build {__PGLENS_BUILD__}</small>
      </header>
      <Outlet />
    </main>
  )
}
