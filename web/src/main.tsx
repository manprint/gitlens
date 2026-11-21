import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// Self-hosted, so the fonts load under the same-origin font-src policy.
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import './index.css'
import { ErrorBoundary } from './components/layout/ErrorBoundary'
import { routes } from './routes'
import { applyTheme } from './hooks/useTheme'
import { activeLocale, refreshLocale, subscribeLocale } from './i18n/store'
import { readPreferences } from './lib/preferences'

// The theme is applied before the first render, so the sign-in page and the
// session check use the operator's theme too, not only the shell.
applyTheme(readPreferences().theme)

const root = document.getElementById('root')

if (!root) {
  throw new Error('pglens root element is missing')
}

const router = createBrowserRouter(routes)
const queryClient = new QueryClient()

// Finding texts come from the server in the console's language (plan 006 F5):
// a language switch refetches what is on screen.
let fetchedLocale = activeLocale()
subscribeLocale(() => {
  if (activeLocale() === fetchedLocale) return
  fetchedLocale = activeLocale()
  void queryClient.invalidateQueries()
})

// A chosen language other than English loads its catalog before the first
// render, so the console never shows half of each language.
void refreshLocale().finally(() => {
  createRoot(root).render(
    <StrictMode>
      <ErrorBoundary>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </ErrorBoundary>
    </StrictMode>,
  )
})
