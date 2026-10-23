import type { ApiFailure } from '@/api/client'

// describeFailure is the sentence a state component shows for a failed request.
export function describeFailure(failure: ApiFailure): string {
  switch (failure.kind) {
    case 'unauthorized':
      return 'Authentication is required.'
    case 'forbidden':
      return 'The current session is not permitted.'
    case 'not_found':
      return 'The requested resource was not found.'
    case 'unprocessable':
      return `${failure.error}: ${failure.detail}`
    case 'server':
      return `Server returned ${failure.status}: ${failure.error}: ${failure.detail}`
    case 'network':
      return `The server could not be reached: ${failure.message}`
    case 'malformed':
      return `The server returned an invalid response: ${failure.message}`
    case 'client':
      return `The page failed to render in the browser: ${failure.message}`
    default: {
      const exhaustive: never = failure
      return exhaustive
    }
  }
}
