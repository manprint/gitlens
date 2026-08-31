import type { RouteObject } from 'react-router-dom'

import App from '@/App'
import LoginPage from '@/features/auth/LoginPage'
import RequireSession from '@/features/auth/RequireSession'

/**
 * The route registry is the single entry point used by the application and by
 * route tests. Phase 7 extends this bootstrap route with the full lazy route
 * tree while the test harness can already mount the real registry today.
 */
export const routes: RouteObject[] = [
  { element: <LoginPage />, path: '/login' },
  {
    element: (
      <RequireSession>
        <App />
      </RequireSession>
    ),
    path: '*',
  },
]
