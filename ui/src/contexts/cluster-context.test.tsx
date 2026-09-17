// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useCluster } from '@/hooks/use-cluster'

import { ClusterProvider } from './cluster-context'

const { refetchClusters, refetchUser, setDefaultCluster } = vi.hoisted(() => ({
  refetchClusters: vi.fn(),
  refetchUser: vi.fn(),
  setDefaultCluster: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  useCurrentClusterList: () => ({ refetch: refetchClusters }),
  useCurrentUser: () => ({ refetch: refetchUser }),
  setDefaultCluster,
}))

function ClusterState() {
  const { currentCluster, isLoading, isSwitching, setCurrentCluster } =
    useCluster()
  const location = useLocation()

  return (
    <>
      <div>
        {isLoading
          ? 'loading'
          : `${currentCluster ?? 'none'}${location.search}`}
      </div>
      <button type="button" onClick={() => setCurrentCluster('mars2')}>
        Switch to mars2
      </button>
      <div data-testid="switch-state">{isSwitching ? 'switching' : 'idle'}</div>
    </>
  )
}

function renderProvider(initialEntry: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <ClusterProvider>
          <ClusterState />
        </ClusterProvider>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('ClusterProvider default selection', () => {
  beforeEach(() => {
    sessionStorage.clear()
    sessionStorage.setItem('current-cluster', 'mars1')
    refetchClusters.mockReset()
    refetchClusters.mockResolvedValue({
      data: [
        {
          id: 1,
          name: 'mars1',
          enabled: true,
          inCluster: false,
          isDefault: false,
          createdAt: '',
          updatedAt: '',
        },
        {
          id: 2,
          name: 'mars2',
          enabled: true,
          inCluster: false,
          isDefault: true,
          createdAt: '',
          updatedAt: '',
        },
      ],
      error: null,
    })
    refetchUser.mockReset()
    refetchUser.mockResolvedValue({
      data: { user: { default_cluster: '' } },
      error: null,
    })
    setDefaultCluster.mockReset()
    setDefaultCluster.mockResolvedValue(undefined)
  })

  it('uses the default cluster when the URL has no cluster parameter', async () => {
    renderProvider('/pods')

    await waitFor(() => {
      expect(screen.getByText('mars2?cluster=mars2')).toBeInTheDocument()
    })
    expect(sessionStorage.getItem('current-cluster')).toBe('mars2')
  })

  it('prefers the user preference over the global default cluster', async () => {
    refetchUser.mockResolvedValue({
      data: { user: { default_cluster: 'mars1' } },
      error: null,
    })

    renderProvider('/pods')

    await waitFor(() => {
      expect(screen.getByText('mars1?cluster=mars1')).toBeInTheDocument()
    })
  })

  it('ignores a user preference that is not an available cluster', async () => {
    refetchUser.mockResolvedValue({
      data: { user: { default_cluster: 'jupiter' } },
      error: null,
    })

    renderProvider('/pods')

    await waitFor(() => {
      expect(screen.getByText('mars2?cluster=mars2')).toBeInTheDocument()
    })
  })

  it('lets the URL cluster parameter win over the user preference', async () => {
    refetchUser.mockResolvedValue({
      data: { user: { default_cluster: 'mars2' } },
      error: null,
    })

    renderProvider('/pods?cluster=mars1')

    await waitFor(() => {
      expect(screen.getByText('mars1?cluster=mars1')).toBeInTheDocument()
    })
  })

  it('persists the user preference when switching clusters', async () => {
    const user = userEvent.setup()
    renderProvider('/pods?cluster=mars1')

    await waitFor(() => {
      expect(screen.getByText('mars1?cluster=mars1')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: 'Switch to mars2' }))

    expect(setDefaultCluster).toHaveBeenCalledWith('mars2')
  })

  it('keeps a visible transition state while switching clusters', async () => {
    const user = userEvent.setup()
    renderProvider('/pods?cluster=mars1')

    await waitFor(() => {
      expect(screen.getByText('mars1?cluster=mars1')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: 'Switch to mars2' }))

    expect(screen.getByTestId('switch-state')).toHaveTextContent('switching')
    expect(screen.getByText('mars2?cluster=mars2')).toBeInTheDocument()
    await waitFor(
      () => {
        expect(screen.getByTestId('switch-state')).toHaveTextContent('idle')
      },
      { timeout: 2000 }
    )
  })
})
