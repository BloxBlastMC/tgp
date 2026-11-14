import { cloneElement, useEffect, useId, useRef, useState, type ReactElement, type ReactNode } from 'react'
import type { Placement } from '../../lib/position'
import { cx } from './cx'
import { mergeRefs, useFloating } from './overlay'

type TriggerProps = {
  ref?: React.Ref<HTMLElement>
  onPointerEnter?: (e: React.PointerEvent) => void
  onPointerLeave?: (e: React.PointerEvent) => void
  onPointerDown?: (e: React.PointerEvent) => void
  onFocus?: (e: React.FocusEvent) => void
  onBlur?: (e: React.FocusEvent) => void
  'aria-describedby'?: string
}

/**
 * Hover/focus hint. Shows after a short delay on hover and straight away on
 * keyboard focus; hides on press so it never covers what you're clicking.
 * `describe` adds it as the trigger's accessible description; leave it off
 * when the content repeats the trigger's label.
 */
export function Tooltip({
  content,
  children,
  side = 'top',
  describe = true,
  delay = 400,
}: {
  content: ReactNode
  children: ReactElement
  side?: Placement
  describe?: boolean
  delay?: number
}) {
  const [open, setOpen] = useState(false)
  const anchor = useRef<HTMLElement | null>(null)
  const panel = useRef<HTMLDivElement>(null)
  const timer = useRef<number | undefined>(undefined)
  const id = useId()
  useFloating(open, anchor, panel, side, { gap: 6 })
  useEffect(() => () => window.clearTimeout(timer.current), [])
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])

  if (content == null || content === '') return children
  const p = children.props as TriggerProps
  const show = (now: boolean) => {
    window.clearTimeout(timer.current)
    if (now) setOpen(true)
    else timer.current = window.setTimeout(() => setOpen(true), delay)
  }
  const hide = () => {
    window.clearTimeout(timer.current)
    setOpen(false)
  }
  const trigger = cloneElement(children as ReactElement<TriggerProps>, {
    ref: mergeRefs(anchor, p.ref),
    onPointerEnter: (e) => {
      p.onPointerEnter?.(e)
      if (e.pointerType === 'mouse') show(false)
    },
    onPointerLeave: (e) => {
      p.onPointerLeave?.(e)
      hide()
    },
    onPointerDown: (e) => {
      p.onPointerDown?.(e)
      hide()
    },
    onFocus: (e) => {
      p.onFocus?.(e)
      if ((e.target as HTMLElement).matches?.(':focus-visible')) show(true)
    },
    onBlur: (e) => {
      p.onBlur?.(e)
      hide()
    },
    'aria-describedby': describe && open ? cx(p['aria-describedby'], id) : p['aria-describedby'],
  })
  return (
    <>
      {trigger}
      {open && (
        <div
          ref={panel}
          popover="manual"
          id={id}
          role="tooltip"
          className="pointer-events-none z-50 max-w-64 animate-fade-in rounded-md border border-border-strong bg-surface-2 px-2 py-1 text-xs font-medium text-fg shadow-popover"
        >
          {content}
        </div>
      )}
    </>
  )
}
