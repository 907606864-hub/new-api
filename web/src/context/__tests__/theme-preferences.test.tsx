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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { DirectionProvider, useDirection } from '@/context/direction-provider'
import { FontProvider, useFont } from '@/context/font-provider'
import { LayoutProvider, useLayout } from '@/context/layout-provider'
import {
  ThemeCustomizationProvider,
  useThemeCustomization,
} from '@/context/theme-customization-provider'
import { ThemeProvider, useTheme } from '@/context/theme-provider'
import { initializeFrontendCache } from '@/lib/frontend-cache'

const savedPreferences = {
  'newapi:theme:v1:mode': 'dark',
  'newapi:theme:v1:preset': 'rose-garden',
  'newapi:theme:v1:font': 'sans',
  'newapi:theme:v1:radius': 'lg',
  'newapi:theme:v1:scale': 'sm',
  'newapi:theme:v1:content-layout': 'centered',
}

function ThemeControls() {
  const theme = useTheme()
  const customization = useThemeCustomization()
  const layout = useLayout()
  const direction = useDirection()
  const font = useFont()

  return (
    <>
      <output aria-label='Theme mode'>{theme.theme}</output>
      <output aria-label='Resolved theme'>{theme.resolvedTheme}</output>
      <output aria-label='Sidebar layout'>
        {layout.collapsible}/{layout.variant}
      </output>
      <button
        type='button'
        onClick={() => {
          theme.setTheme('dark')
          customization.setPreset('rose-garden')
          customization.setFont('sans')
          customization.setRadius('lg')
          customization.setScale('sm')
          customization.setContentLayout('centered')
          layout.setCollapsible('offcanvas')
          layout.setVariant('floating')
          direction.setDir('rtl')
          font.setFont('manrope')
        }}
      >
        Customize
      </button>
      <button
        type='button'
        onClick={() => {
          theme.resetTheme()
          customization.resetCustomization()
          layout.resetLayout()
          direction.resetDir()
          font.resetFont()
        }}
      >
        Reset
      </button>
    </>
  )
}

function ThemeFixture() {
  return (
    <ThemeProvider>
      <FontProvider>
        <DirectionProvider>
          <ThemeCustomizationProvider>
            <LayoutProvider>
              <ThemeControls />
            </LayoutProvider>
          </ThemeCustomizationProvider>
        </DirectionProvider>
      </FontProvider>
    </ThemeProvider>
  )
}

function expectFixedAppearance() {
  expect(screen.getByLabelText('Theme mode')).toHaveTextContent('light')
  expect(screen.getByLabelText('Resolved theme')).toHaveTextContent('light')
  expect(screen.getByLabelText('Sidebar layout')).toHaveTextContent(
    'icon/inset'
  )
  expect(document.documentElement).toHaveClass('light', 'font-inter')
  expect(document.documentElement).not.toHaveClass('dark')
  expect(document.documentElement).not.toHaveClass('font-manrope')
  expect(document.documentElement).toHaveAttribute('dir', 'ltr')
  expect(document.body).toHaveAttribute('data-theme-preset', 'anthropic')
  expect(document.body).toHaveAttribute('data-theme-font', 'serif')
  expect(document.body).not.toHaveAttribute('data-theme-radius')
  expect(document.body).not.toHaveAttribute('data-theme-scale')
  expect(document.body).toHaveAttribute('data-theme-content-layout', 'full')
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
  for (const cookie of document.cookie.split(';')) {
    const name = cookie.trim().split('=')[0]
    document.cookie = `${name}=; path=/; max-age=0`
  }
  document.documentElement.classList.remove(
    'light',
    'dark',
    'font-inter',
    'font-manrope'
  )
  document.documentElement.removeAttribute('dir')
  for (const name of document.body.getAttributeNames()) {
    if (name.startsWith('data-theme-')) document.body.removeAttribute(name)
  }
})

describe('fixed site appearance', () => {
  it('uses light Anthropic for a new visitor', () => {
    render(<ThemeFixture />)

    expectFixedAppearance()
  })

  it('ignores saved theme and legacy layout cookies without clearing unrelated preferences', () => {
    for (const [key, value] of Object.entries(savedPreferences)) {
      localStorage.setItem(key, value)
    }
    localStorage.setItem('unrelated-preference', 'keep')
    for (const cookie of [
      'theme_preset=ocean-breeze',
      'vite-ui-theme=dark',
      'theme_font=sans',
      'theme_radius=xl',
      'theme_scale=lg',
      'theme_content_layout=centered',
      'layout_collapsible=offcanvas',
      'layout_variant=floating',
      'dir=rtl',
      'font=manrope',
    ]) {
      document.cookie = `${cookie}; path=/`
    }
    document.documentElement.classList.add('dark', 'font-manrope')
    document.body.setAttribute('data-theme-radius', 'xl')
    document.body.setAttribute('data-theme-scale', 'lg')

    render(<ThemeFixture />)

    expectFixedAppearance()
    expect(localStorage.getItem('unrelated-preference')).toBe('keep')
  })

  it('stays light when the operating system prefers dark mode', () => {
    const matchMedia = window.matchMedia.bind(window)
    vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
      ...matchMedia(query),
      matches: query === '(prefers-color-scheme: dark)',
    }))
    localStorage.setItem('newapi:theme:v1:mode', 'system')

    render(<ThemeFixture />)

    expectFixedAppearance()
  })

  it('cannot change or reset away from the fixed appearance, including after remounting', async () => {
    const user = userEvent.setup()
    const first = render(<ThemeFixture />)

    await user.click(screen.getByRole('button', { name: 'Customize' }))
    expectFixedAppearance()
    await user.click(screen.getByRole('button', { name: 'Reset' }))
    expectFixedAppearance()
    first.unmount()
    render(<ThemeFixture />)

    expectFixedAppearance()
    expect(document.cookie).toBe('')
    for (const key of Object.keys(savedPreferences)) {
      expect(localStorage.getItem(key)).toBeNull()
    }
  })

  it.each(['', 'unknown'])('ignores invalid saved preferences %j', (value) => {
    for (const key of Object.keys(savedPreferences)) {
      localStorage.setItem(key, value)
    }

    render(<ThemeFixture />)

    expectFixedAppearance()
  })

  it('keeps the fixed appearance when browser storage is unavailable', async () => {
    for (const method of ['getItem', 'setItem', 'removeItem'] as const) {
      vi.spyOn(localStorage, method).mockImplementation(() => {
        throw new DOMException('Storage unavailable', 'SecurityError')
      })
    }
    const user = userEvent.setup()
    render(<ThemeFixture />)

    await user.click(screen.getByRole('button', { name: 'Customize' }))
    await user.click(screen.getByRole('button', { name: 'Reset' }))

    expectFixedAppearance()
  })

  it('ignores old theme preferences even when frontend cache initialization preserves them', () => {
    for (const [key, value] of Object.entries(savedPreferences)) {
      localStorage.setItem(key, value)
    }
    localStorage.setItem('stale-ui-cache', 'old')

    initializeFrontendCache()
    initializeFrontendCache()
    render(<ThemeFixture />)

    expectFixedAppearance()
    expect(localStorage.getItem('stale-ui-cache')).toBeNull()
  })
})
