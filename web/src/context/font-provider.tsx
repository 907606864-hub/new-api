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
import { createContext, useContext, useEffect } from 'react'

import { fonts } from '@/config/fonts'

type Font = (typeof fonts)[number]

type FontContextType = {
  font: Font
  setFont: (font: Font) => void
  resetFont: () => void
}

const fixedFont: FontContextType = {
  font: fonts[0],
  setFont: () => {},
  resetFont: () => {},
}

const FontContext = createContext<FontContextType | null>(null)

export function FontProvider(props: { children: React.ReactNode }) {
  useEffect(() => {
    const root = document.documentElement
    const fontClasses = [...root.classList].filter((cls) =>
      cls.startsWith('font-')
    )
    root.classList.remove(...fontClasses)
    root.classList.add(`font-${fixedFont.font}`)
  }, [])

  return <FontContext value={fixedFont}>{props.children}</FontContext>
}

// eslint-disable-next-line react-refresh/only-export-components
export const useFont = () => {
  const context = useContext(FontContext)
  if (!context) {
    throw new Error('useFont must be used within a FontProvider')
  }
  return context
}
