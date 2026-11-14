import { X } from 'lucide-react'
import { useEffect, useId, useRef, type ReactNode } from 'react'
import { cx } from './cx'

/** Modal dialog built on <dialog> (focus trapping and Esc for free). */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  width = 'max-w-md',
  icon,
}: {
  open: boolean
  onClose: () => void
  title: string
  description?: ReactNode
  children: ReactNode
  footer?: ReactNode
  width?: string
  icon?: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) d.showModal()
    if (!open && d.open) d.close()
  }, [open])
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onClose={onClose}
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
      onMouseDown={(e) => {
        // Click on the backdrop (the dialog element itself, outside its box) closes.
        if (e.target !== ref.current) return
        const r = ref.current.getBoundingClientRect()
        if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) onClose()
      }}
      className={cx(
        'm-auto max-h-[calc(100vh-48px)] w-[calc(100%-32px)] overflow-visible rounded-xl border border-border-strong bg-surface-2 p-0 text-fg shadow-dialog open:animate-pop-in',
        width,
      )}
    >
      {open && (
        <div className="flex max-h-[calc(100vh-50px)] flex-col">
          <header className="flex shrink-0 items-start gap-3 px-5 pt-4 pb-3">
            {icon && <span className="mt-0.5 shrink-0">{icon}</span>}
            <div className="min-w-0 flex-1">
              <h2 id={titleId} className="text-lg font-semibold">
                {title}
              </h2>
              {description && <div className="mt-1 text-fg-muted">{description}</div>}
            </div>
            <button
              type="button"
              onClick={onClose}
              className="-mt-0.5 -mr-1.5 rounded-md p-1.5 text-fg-faint transition-colors hover:bg-bg-hover hover:text-fg"
              aria-label="Close"
            >
              <X size={15} />
            </button>
          </header>
          <div className="flex min-h-0 flex-col gap-3 overflow-y-auto px-5 pb-5">{children}</div>
          {footer && <footer className="flex shrink-0 justify-end gap-2 rounded-b-xl border-t border-border bg-bg-subtle px-5 py-3">{footer}</footer>}
        </div>
      )}
    </dialog>
  )
}

/** Right-aligned action row for the end of a dialog or form. */
export function DialogActions({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cx('-mx-5 mt-2 -mb-5 flex justify-end gap-2 rounded-b-xl border-t border-border bg-bg-subtle px-5 py-3', className)}>{children}</div>
}
