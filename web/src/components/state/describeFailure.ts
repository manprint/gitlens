import type { ApiFailure } from '@/api/client'
import { translate } from '@/i18n/store'

// describeFailure is the sentence a state component shows for a failed request, in the
// console's language (the component re-renders on a language change through useT).
export function describeFailure(failure: ApiFailure): string {
  switch (failure.kind) {
    case 'unauthorized':
      return translate('shell.state.unauthorized')
    case 'forbidden':
      return translate('shell.state.forbidden')
    case 'not_found':
      return translate('shell.state.notFound')
    case 'unprocessable':
      return `${failure.error}: ${failure.detail}`
    case 'server':
      return translate('shell.state.server', {
        status: failure.status,
        error: failure.error,
        detail: failure.detail,
      })
    case 'network':
      return translate('shell.state.network', { message: failure.message })
    case 'malformed':
      return translate('shell.state.malformed', { message: failure.message })
    case 'client':
      return translate('shell.state.client', { message: failure.message })
    default: {
      const exhaustive: never = failure
      return exhaustive
    }
  }
}
