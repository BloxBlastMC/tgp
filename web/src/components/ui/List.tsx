import type { HTMLAttributes, ReactNode } from 'react'
import { cx } from './cx'

/** A bordered card of divided rows. */
export function List({ children, className, ...rest }: HTMLAttributes<HTMLUListElement>) {
  return (
    <ul {...rest} className={cx('overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card', className)}>
      {children}
    </ul>
  )
}

/**
 * One list row: leading icon, title + meta lines, trailing content. Make
 * the whole row clickable by putting a stretched link in `title`
 * (className="after:absolute after:inset-0").
 */
export function ListRow({
  leading,
  title,
  meta,
  trailing,
  className,
  children,
  ...rest
}: Omit<HTMLAttributes<HTMLLIElement>, 'title'> & {
  leading?: ReactNode
  title?: ReactNode
  meta?: ReactNode
  trailing?: ReactNode
} & Record<`data-${string}`, string | undefined>) {
  return (
    <li {...rest} className={cx('relative flex items-start gap-3 border-b border-border px-4 py-2.5 transition-colors last:border-0 hover:bg-bg-hover focus-within:bg-bg-hover has-[a:focus-visible]:outline-2 has-[a:focus-visible]:-outline-offset-2 has-[a:focus-visible]:outline-accent', className)}>
      {leading && <span className="mt-0.5 flex shrink-0">{leading}</span>}
      <div className="min-w-0 flex-1">
        {title}
        {meta && <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm text-fg-muted">{meta}</div>}
        {children}
      </div>
      {trailing && <div className="relative z-[1] flex shrink-0 items-center gap-3 self-center text-sm text-fg-muted">{trailing}</div>}
    </li>
  )
}

/** "·" separator for meta lines. */
export function Sep() {
  return (
    <span aria-hidden className="text-fg-faint">
      ·
    </span>
  )
}
