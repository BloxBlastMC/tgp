import { useId, type ReactNode } from 'react'
import { cx } from './cx'

/**
 * A bordered surface with an optional title row. With a title it's a
 * labelled region; `flush` drops the body padding for tables and lists.
 */
export function Card({
  title,
  description,
  actions,
  footer,
  flush,
  children,
  className,
  bodyClassName,
  id,
  tone,
  icon,
}: {
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  footer?: ReactNode
  flush?: boolean
  children?: ReactNode
  className?: string
  bodyClassName?: string
  id?: string
  tone?: 'danger'
  icon?: ReactNode
}) {
  const titleId = useId()
  return (
    <section
      id={id}
      aria-labelledby={title ? titleId : undefined}
      className={cx(
        'scroll-mt-4 overflow-hidden rounded-lg border bg-surface-1 shadow-card',
        tone === 'danger' ? 'border-danger/35' : 'border-border',
        className,
      )}
    >
      {(title || actions) && (
        <header className={cx('flex items-start justify-between gap-4 border-b px-4 py-3', tone === 'danger' ? 'border-danger/25' : 'border-border')}>
          <div className="flex min-w-0 items-start gap-2.5">
            {icon && <span className="mt-0.5 shrink-0 text-fg-muted">{icon}</span>}
            <div className="min-w-0">
              {title && (
                <h2 id={titleId} className={cx('text-base font-semibold', tone === 'danger' && 'text-danger')}>
                  {title}
                </h2>
              )}
              {description && <p className="mt-0.5 text-sm text-fg-muted">{description}</p>}
            </div>
          </div>
          {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className={cx(!flush && 'p-4', bodyClassName)}>{children}</div>
      {footer && <footer className="flex items-center justify-end gap-2 border-t border-border bg-bg-subtle px-4 py-2.5">{footer}</footer>}
    </section>
  )
}

/** Settings-style section; same as Card. */
export const Section = Card

/** The thin header strip on code/file cards (path, stats, actions). */
export function CardHeaderBar({ children, className, sticky }: { children: ReactNode; className?: string; sticky?: boolean }) {
  return (
    <div
      className={cx(
        'flex min-h-10 items-center gap-2 border-b border-border bg-bg-subtle px-3 py-1.5 text-sm',
        sticky && 'sticky top-0 z-10',
        className,
      )}
    >
      {children}
    </div>
  )
}

/** A titled group in a detail sidebar (Assignees, Labels, Reviewers …). */
export function MetaSection({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="border-b border-border pb-4 last:border-0">
      <div className="mb-2 flex h-6 items-center justify-between text-xs font-semibold tracking-wide text-fg-faint uppercase">
        {title}
        {action}
      </div>
      {children}
    </section>
  )
}

/** Small uppercase heading for groups inside a page or sidebar. */
export function Eyebrow({ children, className, as: As = 'h3' }: { children: ReactNode; className?: string; as?: 'h2' | 'h3' | 'div' | 'p' }) {
  return <As className={cx('text-xs font-semibold tracking-wide text-fg-faint uppercase', className)}>{children}</As>
}
