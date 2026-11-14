import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { computePosition, type Placement, type Side } from '../../lib/position'

/**
 * Calls onDismiss on a pointer press outside every given element. Escape is
 * handled by the panel itself so it can stop the event reaching a parent
 * <dialog>.
 */
export function useDismiss(open: boolean, refs: RefObject<HTMLElement | null>[], onDismiss: () => void) {
  const cb = useRef(onDismiss)
  cb.current = onDismiss
  useEffect(() => {
    if (!open) return
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node
      if (refs.some((r) => r.current?.contains(t))) return
      cb.current()
    }
    document.addEventListener('pointerdown', onDown, true)
    return () => document.removeEventListener('pointerdown', onDown, true)
    // refs are stable ref objects
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])
}

/**
 * Shows `panel` in the top layer (popover="manual") positioned next to
 * `anchor`, keeping it placed while the page scrolls or resizes. The top
 * layer escapes overflow clipping and stacks above an open modal dialog.
 */
export function useFloating(
  open: boolean,
  anchor: RefObject<HTMLElement | null>,
  panel: RefObject<HTMLElement | null>,
  placement: Placement,
  opts: { matchWidth?: boolean; gap?: number } = {},
): Side | null {
  const [side, setSide] = useState<Side | null>(null)
  const { matchWidth, gap } = opts
  const update = useCallback(() => {
    const a = anchor.current
    const p = panel.current
    if (!a || !p) return
    const r = a.getBoundingClientRect()
    if (matchWidth) p.style.minWidth = `${r.width}px`
    const pos = computePosition(r, { width: p.offsetWidth, height: p.offsetHeight }, placement, { width: window.innerWidth, height: window.innerHeight }, gap)
    p.style.top = `${pos.top}px`
    p.style.left = `${pos.left}px`
    setSide((s) => (s === pos.side ? s : pos.side))
  }, [anchor, panel, placement, matchWidth, gap])

  useLayoutEffect(() => {
    if (!open) return
    const p = panel.current
    if (!p) return
    p.style.position = 'fixed'
    try {
      if (p.hasAttribute('popover') && !p.matches(':popover-open')) p.showPopover()
    } catch {
      // popover API unavailable: fixed positioning with a high z-index still works
    }
    update()
    const ro = new ResizeObserver(update)
    ro.observe(p)
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    return () => {
      ro.disconnect()
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
    }
  }, [open, update, panel])
  return side
}

/** Bump a top-layer popover above anything shown after it (e.g. a dialog). */
export function raiseToTop(el: HTMLElement | null) {
  if (!el?.hasAttribute('popover')) return
  try {
    if (el.matches(':popover-open')) el.hidePopover()
    el.showPopover()
  } catch {
    // ignore: not supported
  }
}

/** Merges refs (object or callback) into one callback ref. */
export function mergeRefs<T>(...refs: (React.Ref<T> | undefined)[]): React.RefCallback<T> {
  return (v) => {
    for (const r of refs) {
      if (typeof r === 'function') r(v)
      else if (r) (r as React.RefObject<T | null>).current = v
    }
  }
}

export function isTypingTarget(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null
  return !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
}
