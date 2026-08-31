import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'

import type { ReplicationGraphEdge, TopologyEdge } from '@/lib/replication'

import { TopologyGraph } from './TopologyGraph'

interface MockNode {
  data: Record<string, unknown>
  id: string
  position: { x: number; y: number }
}

interface MockEdge {
  ariaLabel?: string
  id: string
  label?: string
  style?: { strokeDasharray?: string }
}

vi.mock('@xyflow/react', () => ({
  Background: () => null,
  Controls: () => null,
  ReactFlow: ({
    edges,
    nodes,
    onEdgeClick,
    onNodeClick,
    onInit,
  }: {
    edges: MockEdge[]
    nodes: MockNode[]
    onEdgeClick?: (event: React.MouseEvent, edge: MockEdge) => void
    onInit?: (instance: { fitView: () => Promise<boolean> }) => void
    onNodeClick?: (event: React.MouseEvent, node: MockNode) => void
  }) => {
    onInit?.({ fitView: () => Promise.resolve(true) })
    return (
      <div data-testid="mock-react-flow">
        {nodes.map((node) => (
          <button
            data-testid={`topology-node-${node.id}`}
            key={node.id}
            onClick={(event) => onNodeClick?.(event, node)}
            type="button"
          >
            Select node {node.id}
          </button>
        ))}
        {edges.map((edge) => (
          <button
            aria-label={edge.ariaLabel}
            data-edge-style={edge.style?.strokeDasharray ? 'dashed' : 'solid'}
            data-testid={`topology-edge-${edge.id}`}
            key={edge.id}
            onClick={(event) => onEdgeClick?.(event, edge)}
            type="button"
          >
            {edge.label}
          </button>
        ))}
      </div>
    )
  },
}))

const PRIMARY_ID = INSTANCE_ID
const STANDBY_ID = '00000000-0000-4000-8000-000000000002'

const instances: Schemas['InstanceSummary'][] = [
  INSTANCE_SUMMARY,
  {
    ...INSTANCE_SUMMARY,
    addr: 'standby.example.test',
    instance_id: STANDBY_ID,
    role: 'standby',
  },
]

function topology(overrides: Partial<TopologyEdge> = {}): TopologyEdge {
  return {
    confidence: 'high',
    from: PRIMARY_ID,
    sync_state: 'sync',
    to: STANDBY_ID,
    type: 'streaming',
    ...overrides,
  }
}

function renderGraph(
  edge: TopologyEdge = topology(),
  props: Partial<React.ComponentProps<typeof TopologyGraph>> = {},
) {
  return renderWithProviders(<TopologyGraph instances={instances} topology={[edge]} {...props} />)
}

describe('TopologyGraph', () => {
  it('UI-CLUS-001 renders a node per instance and an edge per topology entry', () => {
    renderGraph()

    expect(screen.getAllByTestId(/^topology-node-/)).toHaveLength(instances.length)
    expect(screen.getAllByTestId(/^topology-edge-/)).toHaveLength(1)
  })

  it('UI-CLUS-002 a low-confidence edge is dashed and exposes its note', () => {
    renderGraph(topology({ confidence: 'low', note: 'The standby reported an unknown upstream.' }))

    const edge = screen.getByTestId(/^topology-edge-/)
    expect(edge).toHaveAttribute('data-edge-style', 'dashed')
    expect(edge).toHaveAccessibleName(/unknown upstream/)
  })

  it('UI-CLUS-003 an unknown sync_state renders Unknown, never async', () => {
    renderGraph(topology({ sync_state: null }))

    const edge = screen.getByTestId(/^topology-edge-/)
    expect(edge).toHaveTextContent('Unknown')
    expect(edge).not.toHaveTextContent('async')
  })

  it('UI-CLUS-004 split brain renders a critical banner', () => {
    const secondPrimary: Schemas['InstanceSummary'] = { ...instances[1]!, role: 'primary' }
    renderWithProviders(<TopologyGraph instances={[instances[0]!, secondPrimary]} topology={[]} />)

    expect(screen.getByRole('alert')).toHaveTextContent('Split brain')
  })

  it('UI-CLUS-005 the accessible edge table lists every edge', () => {
    const secondEdge = topology({
      from: STANDBY_ID,
      note: 'Cascading standby',
      to: 'unresolved-instance',
    })
    renderWithProviders(<TopologyGraph instances={instances} topology={[topology(), secondEdge]} />)

    const table = screen.getByRole('table', { name: 'Replication topology edges' })
    expect(within(table).getAllByRole('row')).toHaveLength(3)
    expect(table).toHaveTextContent('Cascading standby')
    expect(table).toHaveTextContent('unresolved-instance')
  })

  it('UI-CLUS-006 selecting a node navigates to the instance route', () => {
    const view = renderGraph()

    fireEvent.click(screen.getByTestId(`topology-node-${PRIMARY_ID}`))

    expect(view.router.state.location.pathname).toBe(`/instances/${PRIMARY_ID}`)
  })

  it('keeps the accessible table hidden until requested and can reveal it', () => {
    renderGraph()

    const toggle = screen.getByRole('button', { name: 'Show accessible edge table' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(toggle)

    expect(screen.getByRole('button', { name: 'Hide accessible edge table' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
    expect(screen.getAllByRole('table', { name: 'Replication topology edges' })).toHaveLength(2)
  })

  it('passes the graph region through the serious and critical a11y checks', async () => {
    vi.useRealTimers()
    renderGraph()
    const graphRegion = screen.getByRole('region', { name: 'Replication topology graph' })

    expect(graphRegion).toBeInTheDocument()
    await expectNoA11yViolations(graphRegion)
  }, 15_000)

  it('selects an edge and highlights its lag chart', () => {
    const edge: ReplicationGraphEdge = {
      confidence: 'high',
      from: PRIMARY_ID,
      id: 'edge-0-primary-standby',
      sync_state: 'sync',
      to: STANDBY_ID,
      type: 'streaming',
    }
    const scrollIntoView = vi.fn()
    const chart = document.createElement('div')
    chart.id = `custom-${edge.id}`
    chart.scrollIntoView = scrollIntoView
    document.body.appendChild(chart)

    renderGraph(topology(), { lagChartId: () => chart.id })
    fireEvent.click(screen.getByTestId(/^topology-edge-/))

    expect(chart).toHaveAttribute('data-highlighted', 'true')
    expect(scrollIntoView).toHaveBeenCalledOnce()
    chart.remove()
  })
})
