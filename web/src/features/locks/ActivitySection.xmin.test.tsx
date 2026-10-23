import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { NOW } from '@/test/time'

import { ActivitySection, type ActivityResponseLike } from './ActivitySection'

const sampled = new Date(NOW.getTime()).toISOString()

function gauge(labels: Record<string, string>) {
  return [{ ts: sampled, value: 0, labels }]
}

function response(extra: Partial<ActivityResponseLike>): ActivityResponseLike {
  return { stale: false, metrics: {}, ...extra }
}

describe('ActivitySection oldest xmin holder', () => {
  it('UI-XMIN-006 names the session holder by kind, pid and age', () => {
    render(
      <ActivitySection
        now={NOW}
        response={response({
          metrics: { pg_xmin_horizon_age_xids: gauge({ holder_kind: 'session' }) },
          xmin_horizon: {
            holder_kind: 'session',
            identity: '4242',
            age_xids: 910,
            age_seconds: 33.5,
            sampled_at: sampled,
          },
        })}
      />,
    )

    expect(screen.getByRole('heading', { name: 'Oldest xmin holder' })).toBeInTheDocument()
    expect(screen.getByText('Holder kind')).toBeInTheDocument()
    expect(screen.getByText('Session')).toBeInTheDocument()
    expect(screen.getByText('PID')).toBeInTheDocument()
    expect(screen.getByText('4242')).toBeInTheDocument()
    expect(screen.getByText('Age in transaction IDs')).toBeInTheDocument()
    expect(screen.getByText('910')).toBeInTheDocument()
    expect(screen.getByText('Time since start')).toBeInTheDocument()
    expect(screen.queryByText(/not measured for this holder kind/)).toBeNull()
  })

  it('UI-XMIN-007 says the time since start is not measured for a slot', () => {
    render(
      <ActivitySection
        now={NOW}
        response={response({
          metrics: { pg_xmin_horizon_age_xids: gauge({ holder_kind: 'slot' }) },
          xmin_horizon: {
            holder_kind: 'slot',
            identity: 'orders_cdc',
            age_xids: 40,
            age_seconds: null,
            sampled_at: sampled,
          },
        })}
      />,
    )

    expect(screen.getByText('Replication slot')).toBeInTheDocument()
    expect(screen.getByText('Slot name')).toBeInTheDocument()
    expect(screen.getByText('orders_cdc')).toBeInTheDocument()
    expect(screen.getByText('not measured for this holder kind')).toBeInTheDocument()
  })

  it('UI-XMIN-008 says the holder is unknown when there is no activity snapshot', () => {
    render(<ActivitySection now={NOW} response={response({})} />)

    expect(screen.getByText('xmin holder unknown: no activity snapshot')).toBeInTheDocument()
  })

  it('UI-XMIN-009 says no holder holds the horizon when the sample reports none', () => {
    render(
      <ActivitySection
        now={NOW}
        response={response({
          metrics: { pg_xmin_horizon_age_xids: gauge({ holder_kind: 'session' }) },
          xmin_horizon: null,
        })}
      />,
    )

    expect(screen.queryByText('xmin holder unknown: no activity snapshot')).toBeNull()
    expect(screen.getByText(/holds an xmin in the latest activity sample/)).toBeInTheDocument()
  })
})
