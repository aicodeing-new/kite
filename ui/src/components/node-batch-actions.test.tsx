import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, vi } from 'vitest'

import { NodeWithMetrics } from '@/types/api'

import { NodeBatchActions } from './node-batch-actions'

const {
  mockCordonNode,
  mockDrainNode,
  mockRestoreNodeScheduling,
  mockTaintNode,
  mockUncordonNode,
  mockUntaintNode,
} = vi.hoisted(() => ({
  mockCordonNode: vi.fn(),
  mockDrainNode: vi.fn(),
  mockRestoreNodeScheduling: vi.fn(),
  mockTaintNode: vi.fn(),
  mockUncordonNode: vi.fn(),
  mockUntaintNode: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  cordonNode: mockCordonNode,
  drainNode: mockDrainNode,
  restoreNodeScheduling: mockRestoreNodeScheduling,
  taintNode: mockTaintNode,
  uncordonNode: mockUncordonNode,
  untaintNode: mockUntaintNode,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}))

function node(
  name: string,
  unschedulable = false,
  taints: { key: string; effect: string }[] = []
): NodeWithMetrics {
  return {
    metadata: { name },
    spec: { unschedulable, taints },
  } as NodeWithMetrics
}

describe('NodeBatchActions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockCordonNode.mockResolvedValue({})
  })

  it('cordons only schedulable nodes and reports skipped nodes', async () => {
    const user = userEvent.setup()
    const refresh = vi.fn().mockResolvedValue(undefined)
    const clearSelection = vi.fn()

    render(
      <NodeBatchActions
        selectedNodes={[node('worker-1'), node('worker-2', true)]}
        clearSelection={clearSelection}
        refresh={refresh}
      />
    )

    await user.click(screen.getByRole('button', { name: /bulk actions/i }))
    await user.click(screen.getByRole('menuitem', { name: 'Cordon' }))

    expect(screen.getByText(/Apply this operation to 1 of 2/)).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Cordon 1 node' }))

    await waitFor(() => expect(mockCordonNode).toHaveBeenCalledTimes(1))
    expect(mockCordonNode).toHaveBeenCalledWith('worker-1')
    expect(screen.getByText('Already cordoned')).toBeVisible()
    expect(refresh).toHaveBeenCalledTimes(1)

    await user.click(screen.getAllByRole('button', { name: 'Close' })[0])
    expect(clearSelection).toHaveBeenCalledTimes(1)
  })

  it('uses serial drain execution by default', async () => {
    const user = userEvent.setup()
    let activeCalls = 0
    let maxActiveCalls = 0
    mockDrainNode.mockImplementation(async () => {
      activeCalls += 1
      maxActiveCalls = Math.max(maxActiveCalls, activeCalls)
      await new Promise((resolve) => setTimeout(resolve, 5))
      activeCalls -= 1
      return { pods: 1 }
    })

    render(
      <NodeBatchActions
        selectedNodes={[node('worker-1'), node('worker-2')]}
        clearSelection={vi.fn()}
        refresh={vi.fn().mockResolvedValue(undefined)}
      />
    )

    await user.click(screen.getByRole('button', { name: /bulk actions/i }))
    await user.click(screen.getByRole('menuitem', { name: 'Drain' }))
    fireEvent.click(screen.getByRole('button', { name: 'Drain 2 nodes' }))

    await waitFor(() => expect(mockDrainNode).toHaveBeenCalledTimes(2))
    expect(maxActiveCalls).toBe(1)
  })

  it('restores every selected node and reports the server results', async () => {
    const user = userEvent.setup()
    const refresh = vi.fn().mockResolvedValue(undefined)
    mockRestoreNodeScheduling.mockImplementation(async (name: string) => ({
      message: 'ok',
      node: name,
      removedTaints: name === 'worker-1' ? 2 : 0,
      uncordoned: name === 'worker-1',
    }))

    render(
      <NodeBatchActions
        selectedNodes={[
          node('worker-1', true, [
            { key: 'example.com/workload', effect: 'NoSchedule' },
            { key: 'example.com/gpu', effect: 'NoExecute' },
          ]),
          node('worker-2'),
        ]}
        clearSelection={vi.fn()}
        refresh={refresh}
      />
    )

    await user.click(screen.getByRole('button', { name: /bulk actions/i }))
    await user.click(
      screen.getByRole('menuitem', { name: 'Restore Scheduling' })
    )

    expect(screen.getByText(/Apply this operation to 2 of 2/)).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Restore Scheduling 2 nodes' })
    )

    await waitFor(() =>
      expect(mockRestoreNodeScheduling).toHaveBeenCalledTimes(2)
    )
    expect(mockRestoreNodeScheduling).toHaveBeenCalledWith('worker-1')
    expect(screen.getByText('2 taints removed, uncordoned')).toBeVisible()
    expect(mockRestoreNodeScheduling).toHaveBeenCalledWith('worker-2')
    expect(screen.getByText('already schedulable')).toBeVisible()
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('restores nodes that look clean locally but have changed on the server', async () => {
    const user = userEvent.setup()
    mockRestoreNodeScheduling.mockImplementation(async (name: string) => ({
      message: 'ok',
      node: name,
      removedTaints: 1,
      uncordoned: true,
    }))

    render(
      <NodeBatchActions
        selectedNodes={[node('worker-1'), node('worker-2')]}
        clearSelection={vi.fn()}
        refresh={vi.fn().mockResolvedValue(undefined)}
      />
    )

    await user.click(screen.getByRole('button', { name: /bulk actions/i }))
    await user.click(
      screen.getByRole('menuitem', { name: 'Restore Scheduling' })
    )

    expect(screen.getByText(/Apply this operation to 2 of 2/)).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Restore Scheduling 2 nodes' })
    )
    await waitFor(() =>
      expect(screen.getAllByText('1 taint removed, uncordoned')).toHaveLength(2)
    )
    expect(mockRestoreNodeScheduling).toHaveBeenCalledWith('worker-1')
    expect(mockRestoreNodeScheduling).toHaveBeenCalledWith('worker-2')
  })

  it('retries only failed restores without repeating successful nodes', async () => {
    const user = userEvent.setup()
    const refresh = vi.fn().mockResolvedValue(undefined)
    let failedOnce = false
    mockRestoreNodeScheduling.mockImplementation(async (name: string) => {
      if (name === 'worker-2' && !failedOnce) {
        failedOnce = true
        throw new Error('temporary failure')
      }
      return { message: 'ok', node: name, removedTaints: 1, uncordoned: true }
    })
    render(
      <NodeBatchActions
        selectedNodes={[node('worker-1'), node('worker-2')]}
        clearSelection={vi.fn()}
        refresh={refresh}
      />
    )
    await user.click(screen.getByRole('button', { name: /bulk actions/i }))
    await user.click(
      screen.getByRole('menuitem', { name: 'Restore Scheduling' })
    )
    await user.click(
      screen.getByRole('button', { name: 'Restore Scheduling 2 nodes' })
    )
    await user.click(
      await screen.findByRole('button', { name: 'Retry failed (1)' })
    )
    await waitFor(() =>
      expect(screen.getAllByText('1 taint removed, uncordoned')).toHaveLength(2)
    )
    expect(mockRestoreNodeScheduling.mock.calls.map(([name]) => name)).toEqual([
      'worker-1',
      'worker-2',
      'worker-2',
    ])
    expect(refresh).toHaveBeenCalledTimes(2)
  })
})
