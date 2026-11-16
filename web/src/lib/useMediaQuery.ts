import { useSyncExternalStore } from 'react'

/** Live result of a CSS media query (false where matchMedia is missing). */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (on) => {
      if (typeof window === 'undefined' || !window.matchMedia) return () => {}
      const m = window.matchMedia(query)
      m.addEventListener('change', on)
      return () => m.removeEventListener('change', on)
    },
    () => (typeof window !== 'undefined' && window.matchMedia ? window.matchMedia(query).matches : false),
    () => false,
  )
}
