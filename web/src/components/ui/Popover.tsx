import { useCallback, useId, useRef, useState, type ReactElement, type ReactNode } from 'react'
import type { Placement } from '../../lib/position'
import { cx } from './cx'
import { useDismiss, useFloating } from './overlay'

export type TriggerProps = {
  ref: React.RefCallback<HTMLElement>
  onClick: (e: React.MouseEvent) => void
  'aria-expanded': boolean
  'aria-controls'?: string
  'aria-haspopup'?: 'menu' | 'listbox' | 'dialog' | true
}

/**
 * The floating panel itself: rendered only while open, shown in the top
 * layer, positioned next to `anchor`, closed on outside press and Escape
 * (Escape returns focus to the anchor).
 */
export function FloatingPanel({
  open,
  onClose,
  anchor,
  placement = 'bottom-start',
  className,
  children,
  matchWidth,
  role,
  id,
  label,
  onKeyDown,
  panelRef,
}: {
  open: boolean
  onClose: () => void
  anchor: React.RefObject<HTMLElement | null>
  placement?: Placement
  className?: string
  children: ReactNode
  matchWidth?: boolean
  role?: string
  id?: string
  label?: string
  onKeyDown?: (e: React.KeyboardEvent<HTMLDivElement>) => void
  panelRef?: React.RefObject<HTMLDivElement | null>
}) {
  const own = useRef<HTMLDivElement>(null)
  const panel = panelRef ?? own
  useFloating(open, anchor, panel, placement, { matchWidth })
  useDismiss(open, [panel, anchor], onClose)
  if (!open) return null
  return (
    <div
      ref={panel}
      popover="manual"
      id={id}
      role={role}
      aria-label={label}
      onKeyDown={(e) => {
        onKeyDown?.(e)
        if (e.defaultPrevented) return
        if (e.key === 'Escape') {
          e.preventDefault()
          e.stopPropagation()
          onClose()
          anchor.current?.focus()
        }
      }}
      className={cx('z-50 animate-pop-in rounded-lg border border-border-strong bg-surface-2 text-fg shadow-popover', className)}
    >
      {children}
    </div>
  )
}

/** Open/close state plus the anchor ref for a popover trigger. */
export function usePopover() {
  const [open, setOpen] = useState(false)
  const anchor = useRef<HTMLElement | null>(null)
  const id = useId()
  const close = useCallback(() => setOpen(false), [])
  const triggerProps = (haspopup: TriggerProps['aria-haspopup'] = 'dialog'): TriggerProps => ({
    ref: (el) => {
      anchor.current = el
    },
    onClick: () => setOpen((o) => !o),
    'aria-expanded': open,
    'aria-controls': open ? id : undefined,
    'aria-haspopup': haspopup,
  })
  return { open, setOpen, close, anchor, id, triggerProps }
}

/** A trigger plus floating content. */
export function Popover({
  trigger,
  children,
  placement,
  className,
  label,
}: {
  trigger: (props: TriggerProps) => ReactElement
  children: ReactNode | ((close: () => void) => ReactNode)
  placement?: Placement
  className?: string
  label?: string
}) {
  const p = usePopover()
  return (
    <>
      {trigger(p.triggerProps('dialog'))}
      <FloatingPanel open={p.open} onClose={p.close} anchor={p.anchor} placement={placement} className={className} id={p.id} role="dialog" label={label}>
        {typeof children === 'function' ? children(p.close) : children}
      </FloatingPanel>
    </>
  )
}
