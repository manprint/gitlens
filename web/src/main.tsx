import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import './index.css'
import { ErrorBoundary } from './components/layout/ErrorBoundary'
import { routes } from './routes'

const root = document.getElementById('root')

if (!root) {
  throw new Error('pglens root element is missing')
}

const router = createBrowserRouter(routes)
const queryClient = new QueryClient()

createRoot(root).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
)
