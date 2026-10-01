/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import { SidebarProvider } from '@/components/ui/sidebar'
import { DirectionProvider } from '@/context/direction-provider'
import { LayoutProvider } from '@/context/layout-provider'
import { SearchProvider } from '@/context/search-provider'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { AppHeader } from '../app-header'
import { PublicHeader } from '../public-header'

const originalGetAnimations = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'getAnimations'
)

beforeAll(() => {
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})

afterAll(() => {
  if (originalGetAnimations) {
    Object.defineProperty(
      HTMLElement.prototype,
      'getAnimations',
      originalGetAnimations
    )
    return
  }
  Reflect.deleteProperty(HTMLElement.prototype, 'getAnimations')
})

describe('appearance controls', () => {
  let client: QueryClient

  beforeEach(() => {
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(['status'], {
      system_name: 'New API',
      version: 'v1.0.0',
    })
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'test-user',
      role: 1,
    })
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/notice') {
        return { data: { success: true, data: '' } }
      }
      return { data: { success: true, data: [] } }
    })
  })

  afterEach(() => {
    cleanup()
    client.clear()
    useAuthStore.getState().auth.reset()
    vi.restoreAllMocks()
  })

  it('renders AppHeader without theme toggle or config drawer trigger', async () => {
    const root = createRootRoute()
    const index = createRoute({
      getParentRoute: () => root,
      path: '/',
      component: () => (
        <DirectionProvider>
          <LayoutProvider>
            <SidebarProvider>
              <SearchProvider>
                <AppHeader />
              </SearchProvider>
            </SidebarProvider>
          </LayoutProvider>
        </DirectionProvider>
      ),
    })
    const router = createRouter({
      routeTree: root.addChildren([index]),
      history: createMemoryHistory({ initialEntries: ['/'] }),
    })
    await router.load()

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )

    expect(await screen.findByRole('banner')).toBeVisible()
    expect(
      screen.queryAllByRole('button', { name: /Toggle theme/i })
    ).toHaveLength(0)
    expect(
      screen.queryAllByRole('button', { name: /Open theme settings/i })
    ).toHaveLength(0)
  })

  it('renders PublicHeader without desktop or mobile theme switches', async () => {
    const root = createRootRoute()
    const index = createRoute({
      getParentRoute: () => root,
      path: '/',
      component: () => <PublicHeader />,
    })
    const router = createRouter({
      routeTree: root.addChildren([index]),
      history: createMemoryHistory({ initialEntries: ['/'] }),
    })
    await router.load()

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )

    expect(await screen.findByRole('banner')).toBeVisible()
    expect(
      screen.queryAllByRole('button', { name: /Toggle theme/i })
    ).toHaveLength(0)
  })

  it('opens CommandMenu via keyboard search shortcut and excludes theme controls while keeping navigation', async () => {
    const user = userEvent.setup()
    const root = createRootRoute()
    const index = createRoute({
      getParentRoute: () => root,
      path: '/',
      component: () => (
        <SearchProvider>
          <button type='button'>trigger</button>
        </SearchProvider>
      ),
    })
    const router = createRouter({
      routeTree: root.addChildren([index]),
      history: createMemoryHistory({ initialEntries: ['/'] }),
    })
    await router.load()

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )

    await user.keyboard('{Control>}k{/Control}')

    expect(
      await screen.findByPlaceholderText(/Type a command or search.../i)
    ).toBeInTheDocument()
    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(screen.queryByText('Theme')).not.toBeInTheDocument()
    expect(screen.queryByText('Light')).not.toBeInTheDocument()
    expect(screen.queryByText('Dark')).not.toBeInTheDocument()
    expect(screen.queryByText('System')).not.toBeInTheDocument()

    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })
})
