// @vitest-environment jsdom

import { ClusterContext } from '@/contexts/cluster-context'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ClusterAwareLinks } from './cluster-aware-links'
import { ClusterSelector } from './cluster-selector'

describe('ClusterSelector new-tab action', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/pods?cluster=mars1')
  })

  const clusterContextValue = (
    overrides?: Partial<{
      userDefaultCluster: string
      toggleUserDefaultCluster: ReturnType<typeof vi.fn>
    }>
  ) => ({
    clusters: [
      {
        id: 1,
        name: 'mars1',
        enabled: true,
        inCluster: false,
        isDefault: true,
        createdAt: '',
        updatedAt: '',
      },
      {
        id: 2,
        name: 'mars2',
        enabled: true,
        inCluster: false,
        isDefault: false,
        createdAt: '',
        updatedAt: '',
      },
    ],
    currentCluster: 'mars1',
    setCurrentCluster: vi.fn(),
    userDefaultCluster: overrides?.userDefaultCluster ?? '',
    toggleUserDefaultCluster: overrides?.toggleUserDefaultCluster ?? vi.fn(),
    isLoading: false,
    isSwitching: false,
    error: null,
  })

  it('renders a native new-tab link for the selected target cluster', async () => {
    const user = userEvent.setup()
    render(
      <ClusterContext.Provider value={clusterContextValue()}>
        <ClusterAwareLinks />
        <ClusterSelector />
      </ClusterContext.Provider>
    )

    expect(screen.getByRole('button', { name: /mars1/i })).toHaveClass('w-full')
    await user.click(screen.getByRole('button', { name: /mars1/i }))
    const newTabLink = screen.getByRole('menuitem', {
      name: 'Open mars2 in a new tab',
    })

    expect(newTabLink).toHaveAttribute('href', '/pods?cluster=mars2')
    expect(newTabLink).toHaveAttribute('target', '_blank')
    expect(newTabLink).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('offers a pin action that toggles the user default cluster', async () => {
    const user = userEvent.setup()
    const toggleUserDefaultCluster = vi.fn()
    render(
      <ClusterContext.Provider
        value={clusterContextValue({
          userDefaultCluster: 'mars1',
          toggleUserDefaultCluster,
        })}
      >
        <ClusterSelector />
      </ClusterContext.Provider>
    )

    await user.click(screen.getByRole('button', { name: /mars1/i }))
    expect(
      screen.getByRole('menuitem', {
        name: 'Clear mars1 as my default cluster',
      })
    ).toBeInTheDocument()

    await user.click(
      screen.getByRole('menuitem', {
        name: 'Set mars2 as my default cluster',
      })
    )
    expect(toggleUserDefaultCluster).toHaveBeenCalledWith('mars2')
  })
})
