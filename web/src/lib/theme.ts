import { useCallback, useSyncExternalStore } from 'react'

export type Theme = 'dark' | 'light'
/** What the user picked; "system" follows the OS. */
export type ThemePref = Theme | 'system'

const KEY = 'tgp-theme'
const listeners = new Set<() => void>()
const media = typeof window !== 'undefined' ? window.matchMedia('(prefers-color-scheme: light)') : null

function readPref(): ThemePref {
  const t = document.documentElement.dataset.theme
  return t === 'light' || t === 'dark' ? t : 'system'
}

function resolved(pref: ThemePref): Theme {
  if (pref !== 'system') return pref
  return media?.matches ? 'light' : 'dark'
}

function emit() {
  for (const l of listeners) l()
}

media?.addEventListener('change', emit)

export function setThemePref(pref: ThemePref) {
  if (pref === 'system') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = pref
  try {
    if (pref === 'system') localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, pref)
  } catch {
    // storage unavailable; the choice still applies for this session
  }
  emit()
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

const snapshot = () => `${readPref()}:${resolved(readPref())}`

/** Theme preference shared by every component, persisted to localStorage. */
export function useTheme(): { pref: ThemePref; theme: Theme; setPref: (p: ThemePref) => void; toggle: () => void } {
  const snap = useSyncExternalStore(subscribe, snapshot)
  const [pref, theme] = snap.split(':') as [ThemePref, Theme]
  const toggle = useCallback(() => setThemePref(resolved(readPref()) === 'dark' ? 'light' : 'dark'), [])
  return { pref, theme, setPref: setThemePref, toggle }
}
