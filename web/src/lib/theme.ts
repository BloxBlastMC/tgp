import { useCallback, useState } from 'react'

export type Theme = 'dark' | 'light'

function current(): Theme {
  const t = document.documentElement.dataset.theme
  if (t === 'light' || t === 'dark') return t
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

/** Theme state persisted to localStorage and applied as <html data-theme>. */
export function useTheme(): [Theme, () => void] {
  const [theme, setTheme] = useState<Theme>(current)
  const toggle = useCallback(() => {
    const next: Theme = current() === 'dark' ? 'light' : 'dark'
    document.documentElement.dataset.theme = next
    try {
      localStorage.setItem('tgp-theme', next)
    } catch {
      // storage unavailable; theme still applies for this session
    }
    setTheme(next)
  }, [])
  return [theme, toggle]
}
