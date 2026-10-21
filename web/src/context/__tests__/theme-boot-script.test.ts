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
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { THEME_STORAGE_KEYS } from '@/lib/theme-storage'

// One-shot detectors must see the same fixed theme before React mounts as
// ThemeProvider applies afterward, regardless of saved or system preferences.
const html = readFileSync(
  resolve(import.meta.dirname, '../../../index.html'),
  'utf8'
)
const bootScript = html.match(/<script>([\s\S]*?)<\/script>/)?.[1] ?? ''

function runBootScript(): void {
  new Function(bootScript)()
}

function mockSystemDarkMode(dark: boolean): void {
  const matchMedia = window.matchMedia.bind(window)
  vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => ({
    ...matchMedia(query),
    matches: dark && query === '(prefers-color-scheme: dark)',
  }))
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  document.documentElement.classList.remove('light', 'dark', 'font-inter')
})

describe('index.html theme boot script', () => {
  it.each([
    { saved: 'dark', systemDark: false },
    { saved: 'light', systemDark: true },
    { saved: 'system', systemDark: true },
    { saved: null, systemDark: true },
    { saved: null, systemDark: false },
    { saved: 'sepia', systemDark: true },
  ])(
    'applies fixed light before React mounts with saved=$saved and systemDark=$systemDark',
    ({ saved, systemDark }) => {
      mockSystemDarkMode(systemDark)
      if (saved !== null) localStorage.setItem(THEME_STORAGE_KEYS.mode, saved)
      localStorage.setItem('unrelated-preference', 'keep')

      runBootScript()

      expect(document.documentElement).toHaveClass('light')
      expect(document.documentElement).not.toHaveClass('dark')
      expect(localStorage.getItem(THEME_STORAGE_KEYS.mode)).toBe(saved)
      expect(localStorage.getItem('unrelated-preference')).toBe('keep')
    }
  )

  it('keeps first paint light when browser storage and system preferences are unavailable', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('Storage unavailable', 'SecurityError')
    })
    vi.spyOn(window, 'matchMedia').mockImplementation(() => {
      throw new Error('System preference unavailable')
    })

    expect(runBootScript).not.toThrow()

    expect(document.documentElement).toHaveClass('light')
    expect(document.documentElement).not.toHaveClass('dark')
  })

  it('removes a stale dark class without changing unrelated classes when repeated', () => {
    mockSystemDarkMode(false)
    document.documentElement.classList.add('dark', 'font-inter')

    runBootScript()
    runBootScript()

    expect(document.documentElement).toHaveClass('light', 'font-inter')
    expect(document.documentElement).not.toHaveClass('dark')
  })
})
