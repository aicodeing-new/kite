// @vitest-environment jsdom

import { useEffect } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  createBrowserRouter,
  MemoryRouter,
  RouterProvider,
  useLocation,
} from 'react-router-dom'
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

function ClusterState({
  onSelection,
}: {
  onSelection?: (cluster: string | null, search: string) => void
}) {
  const {
    currentCluster,
    isLoading,
    isSwitching,
    setCurrentCluster,
    userDefaultCluster,
    toggleUserDefaultCluster,
  } = useCluster()
  const location = useLocation()

  useEffect(() => {
    onSelection?.(currentCluster, location.search)
  }, [currentCluster, location.search, onSelection])

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
      <button type="button" onClick={() => toggleUserDefaultCluster('mars1')}>
        Pin mars1
      </button>
      <div data-testid="user-default">{userDefaultCluster || 'none'}</div>
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
    window.history.replaceState({}, '', '/')
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

  it('does not change the preference when switching clusters', async () => {
    const user = userEvent.setup()
    renderProvider('/pods?cluster=mars1')

    await waitFor(() => {
      expect(screen.getByText('mars1?cluster=mars1')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: 'Switch to mars2' }))

    expect(screen.getByText('mars2?cluster=mars2')).toBeInTheDocument()
    expect(setDefaultCluster).not.toHaveBeenCalled()
  })

  it('keeps the selected cluster after a browser router transition completes', async () => {
    window.history.replaceState({}, '', '/pods?cluster=mars1&namespace=dev#top')
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const onSelection = vi.fn()
    const router = createBrowserRouter([
      {
        path: '/pods',
        element: (
          <ClusterProvider>
            <ClusterState onSelection={onSelection} />
          </ClusterProvider>
        ),
      },
    ])
    const view = render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    const user = userEvent.setup()

    try {
      await screen.findByText('mars1?cluster=mars1&namespace=dev')
      onSelection.mockClear()
      await user.click(screen.getByRole('button', { name: 'Switch to mars2' }))
      await waitFor(() => {
        expect(screen.getByTestId('switch-state')).toHaveTextContent('idle')
      })

      expect(
        screen.getByText('mars2?cluster=mars2&namespace=dev')
      ).toBeInTheDocument()
      expect(sessionStorage.getItem('current-cluster')).toBe('mars2')
      expect(window.location.search).toBe('?cluster=mars2&namespace=dev')
      expect(window.location.hash).toBe('#top')
      // A new cluster must never mount against the previous browser URL:
      // API headers and query keys read their cluster from that URL.
      expect(onSelection).not.toHaveBeenCalledWith(
        'mars2',
        '?cluster=mars1&namespace=dev'
      )
    } finally {
      view.unmount()
      router.dispose()
      queryClient.clear()
    }
  })

  it('pins and unpins the default cluster via the explicit action', async () => {
    const user = userEvent.setup()
    renderProvider('/pods')

    await waitFor(() => {
      expect(screen.getByText('mars2?cluster=mars2')).toBeInTheDocument()
    })
    expect(screen.getByTestId('user-default')).toHaveTextContent('none')

    await user.click(screen.getByRole('button', { name: 'Pin mars1' }))
    expect(setDefaultCluster).toHaveBeenCalledWith('mars1')
    expect(screen.getByTestId('user-default')).toHaveTextContent('mars1')

    await user.click(screen.getByRole('button', { name: 'Pin mars1' }))
    expect(setDefaultCluster).toHaveBeenCalledWith('')
    expect(screen.getByTestId('user-default')).toHaveTextContent('none')
  })

  it('reverts the pin when persisting fails', async () => {
    setDefaultCluster.mockRejectedValueOnce(new Error('boom'))
    const user = userEvent.setup()
    renderProvider('/pods')

    await waitFor(() => {
      expect(screen.getByText('mars2?cluster=mars2')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: 'Pin mars1' }))

    // The optimistic pin is applied and then rolled back after the failed
    // write, leaving the user on "follow the global default".
    expect(setDefaultCluster).toHaveBeenCalledWith('mars1')
    await waitFor(() => {
      expect(screen.getByTestId('user-default')).toHaveTextContent('none')
    })
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
