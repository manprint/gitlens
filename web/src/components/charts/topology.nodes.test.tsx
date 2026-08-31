import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { NodeProps } from '@xyflow/react'

import { INSTANCE_ID } from '@/test/fixture-helpers'

import { TopologyNode, type TopologyFlowNode, type TopologyNodeData } from './topology.nodes'

vi.mock('@xyflow/react', () => ({
  Handle: () => null,
  Position: { Bottom: 'bottom', Top: 'top' },
}))

function nodeProps(data: TopologyNodeData): NodeProps<TopologyFlowNode> {
  return { data } as unknown as NodeProps<TopologyFlowNode>
}

const baseNode: TopologyNodeData = {
  address: 'postgres.example.test',
  id: INSTANCE_ID,
  port: 5432,
  role: 'standby',
  tier: 'T1',
  up: true,
  version: 16,
}

describe('TopologyNode', () => {
  it('shows standby metadata and replay lag', () => {
    render(<TopologyNode {...nodeProps({ ...baseNode, replayLagSec: 1.25 })} />)

    expect(screen.getByText('standby')).toBeInTheDocument()
    expect(screen.getByText('postgres.example.test')).toBeInTheDocument()
    expect(screen.getByText('1.250 s')).toBeInTheDocument()
    expect(screen.getByLabelText(/standby node.*Up/)).toBeInTheDocument()
  })

  it('makes a down instance unmistakable and labels an unresolved endpoint', () => {
    const { rerender } = render(
      <TopologyNode {...nodeProps({ ...baseNode, up: false, replayLagSec: null })} />,
    )
    expect(screen.getByText('Down')).toBeInTheDocument()
    expect(screen.getByLabelText(/standby node.*Down/)).toBeInTheDocument()

    rerender(
      <TopologyNode
        {...nodeProps({
          ...baseNode,
          address: null,
          id: 'unresolved:missing-instance',
          role: 'unresolved',
          port: null,
          tier: null,
          up: null,
          version: null,
        })}
      />,
    )
    expect(screen.getByText('Unresolved endpoint missing-instance')).toBeInTheDocument()
    expect(screen.getAllByText('Unknown')).toHaveLength(4)
  })
})
