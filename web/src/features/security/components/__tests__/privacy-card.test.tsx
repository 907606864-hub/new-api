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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Toaster, toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { UserProfile } from '@/features/profile/types'
import { api } from '@/lib/api'

import { PrivacyCard } from '../privacy-card'

const profile: UserProfile = {
  id: 1,
  username: 'alice',
  display_name: 'Alice',
  role: 1,
  group: 'default',
  quota: 1000000,
  used_quota: 0,
  request_count: 0,
  status: 1,
  aff_count: 0,
  aff_quota: 0,
  aff_history_quota: 0,
  created_time: 0,
  setting: JSON.stringify({ record_ip_log: true }),
}

afterEach(() => {
  toast.dismiss()
  vi.restoreAllMocks()
})

function renderPrivacy(setting = profile.setting) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const onUpdate = vi.fn()
  const rendered = render(
    <QueryClientProvider client={client}>
      <PrivacyCard profile={{ ...profile, setting }} onUpdate={onUpdate} />
      <Toaster />
    </QueryClientProvider>
  )
  return { ...rendered, onUpdate }
}

describe('privacy settings', () => {
  it('shows the mandatory policy even when the saved profile disabled IP logging', () => {
    renderPrivacy(JSON.stringify({ record_ip_log: false }))
    const toggle = screen.getByRole('switch', { name: 'Record IP Address' })
    expect(toggle).toBeChecked()
    expect(toggle).toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.getByText('Mandatory security policy: IP logging is permanently enabled by administrator.')
    ).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Save Settings' })).not.toBeInTheDocument()
  })

  it('mouse clicks cannot disable IP logging or submit settings', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: profile } })
    const put = vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    const { onUpdate } = renderPrivacy()
    const toggle = screen.getByRole('switch', { name: 'Record IP Address' })
    await user.click(toggle)
    expect(toggle).toBeChecked()
    expect(get).not.toHaveBeenCalled()
    expect(put).not.toHaveBeenCalled()
    expect(onUpdate).not.toHaveBeenCalled()
  })

  it('keyboard input cannot disable IP logging or submit settings', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: profile } })
    const put = vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    const { onUpdate } = renderPrivacy()
    const toggle = screen.getByRole('switch', { name: 'Record IP Address' })
    expect(toggle).toHaveAttribute('tabindex', '-1')
    toggle.focus()
    await user.keyboard(' ')
    expect(toggle).toBeChecked()
    expect(get).not.toHaveBeenCalled()
    expect(put).not.toHaveBeenCalled()
    expect(onUpdate).not.toHaveBeenCalled()
  })
})
