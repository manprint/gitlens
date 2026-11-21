import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import { PlanSamples } from './PlanSamples'
import { dataRows } from '@/test/table'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const SAMPLES = 'Plan samples'

function sample(ts: string, duration: number, hasParameters: boolean, parameters?: string) {
  return {
    ts,
    datname: 'app',
    queryid: '77',
    plan_hash: 'h1',
    duration_ms: duration,
    analyzed: false,
    has_parameters: hasParameters,
    ...(parameters === undefined ? {} : { parameters }),
  }
}

function renderSamples() {
  return renderWithProviders(
    <PlanSamples instanceId={INSTANCE_ID} queryid="77" planHash="h1" datname="app" />,
    { route: `/instances/${INSTANCE_ID}/queries/77` },
  )
}

describe('PlanSamples', () => {
  it('UI-AEX-002 lists samples and asks for parameters only after the button', async () => {
    vi.useRealTimers()
    const requests: URLSearchParams[] = []
    server.use(
      http.get('*/api/v1/plans/samples', ({ request }) => {
        const params = new URL(request.url).searchParams
        requests.push(params)
        const include = params.get('include_parameters') === 'true'
        return HttpResponse.json({
          samples: [
            sample('2024-01-02T00:00:00Z', 12.5, true, include ? "$1 = '42'" : undefined),
            sample('2024-01-01T00:00:00Z', 3, false),
          ],
        })
      }),
    )
    const { container } = renderSamples()
    await waitFor(() => expect(dataRows(SAMPLES)).toHaveLength(2))
    expect(requests[0]?.get('include_parameters')).toBeNull()
    expect(requests[0]?.get('plan_hash')).toBe('h1')
    expect(requests[0]?.get('database')).toBe('app')
    expect(dataRows(SAMPLES)[0]).toHaveTextContent('Hidden')
    expect(dataRows(SAMPLES)[1]).toHaveTextContent('Not stored')
    expect(screen.queryByText("$1 = '42'")).not.toBeInTheDocument()
    await expectNoA11yViolations(container)

    fireEvent.click(screen.getByRole('button', { name: 'Show parameters' }))
    expect(await screen.findByText("$1 = '42'")).toBeInTheDocument()
    expect(requests.at(-1)?.get('include_parameters')).toBe('true')
  })

  it('UI-AEX-003 an empty list names the settings to enable', async () => {
    vi.useRealTimers()
    server.use(http.get('*/api/v1/plans/samples', () => HttpResponse.json({ samples: [] })))
    renderSamples()
    const setup = await screen.findByTestId('auto-plan-setup')
    for (const setting of [
      'auto_explain',
      'auto_explain.log_min_duration',
      'auto_explain.log_format = json',
      'compute_query_id = on',
      'logging_collector = on',
      "log_destination = 'jsonlog'",
      'checks.logs.enabled',
    ]) {
      expect(setup).toHaveTextContent(setting)
    }
  })
})
