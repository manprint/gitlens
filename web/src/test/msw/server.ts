import { setupServer } from 'msw/node'

/**
 * Tests opt into handlers explicitly. The setup file turns unhandled requests
 * into failures so a component cannot silently pass with an incomplete mock.
 */
export const server = setupServer()
