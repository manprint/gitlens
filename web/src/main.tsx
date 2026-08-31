import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'

import './index.css'
import { routes } from './routes'

const root = document.getElementById('root')

if (!root) {
  throw new Error('pglens root element is missing')
}

const router = createBrowserRouter(routes)

createRoot(root).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
