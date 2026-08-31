import { NavLink } from 'react-router-dom'

const primaryDestinations = [
  { icon: '⌂', label: 'Fleet', path: '/' },
  { icon: '✦', label: 'Findings', path: '/findings' },
  { icon: '!', label: 'Alerts', path: '/alerts' },
  { icon: '⚙', label: 'Settings', path: '/settings' },
] as const

export function Sidebar() {
  return (
    <aside
      className="bg-surface fixed inset-y-0 left-0 z-20 w-16 border-r px-2 py-4 lg:w-64"
      data-collapsed-below="1024px"
      data-testid="app-sidebar"
    >
      <nav aria-label="Primary navigation">
        <ul className="flex flex-col gap-2">
          {primaryDestinations.map(({ icon, label, path }) => (
            <li key={path}>
              <NavLink
                aria-label={label}
                className={({ isActive }) =>
                  `flex min-h-10 items-center gap-3 rounded-md px-3 py-2 text-sm ${
                    isActive
                      ? 'bg-surface-raised text-text-primary font-semibold'
                      : 'text-text-secondary hover:bg-surface-raised'
                  }`
                }
                end={path === '/'}
                to={path}
              >
                <span aria-hidden="true" className="w-5 text-center">
                  {icon}
                </span>
                <span className="hidden lg:inline">{label}</span>
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>
    </aside>
  )
}
