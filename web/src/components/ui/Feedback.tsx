import { Loader2 } from 'lucide-react'
import type { ReactNode } from 'react'
import { cx } from './cx'

export function Spinner({ inline, size = 18, className, label = 'Loading' }: { inline?: boolean; size?: number; className?: string; label?: string }) {
  const icon = <Loader2 className="animate-spin" size={inline ? 13 : size} aria-hidden />
  if (inline)
    return (
      <span role="status" aria-label={label} className={cx('inline-flex text-fg-faint', className)}>
        {icon}
      </span>
    )
  return (
    <div role="status" aria-label={label} className={cx('flex justify-center p-10 text-fg-faint', className)}>
      {icon}
    </div>
  )
}

export function Skeleton({ className, style }: { className?: string; style?: React.CSSProperties }) {
  return <div aria-hidden style={style} className={cx('animate-shimmer rounded bg-bg-active', className)} />
}

/** Placeholder list rows while data loads. */
export function SkeletonRows({ rows = 5, className, avatar }: { rows?: number; className?: string; avatar?: boolean }) {
  return (
    <div role="status" aria-label="Loading" className={className}>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-center gap-3 border-b border-border px-4 py-3 last:border-0">
          {avatar ? <Skeleton className="size-5 rounded-full" /> : <Skeleton className="size-4 rounded-full" />}
          <div className="flex flex-1 flex-col gap-1.5">
            <Skeleton className="h-3" style={{ width: `${55 + ((i * 17) % 35)}%` }} />
            <Skeleton className="h-2.5 w-1/3" />
          </div>
        </div>
      ))}
    </div>
  )
}

/** A page-level loading placeholder: header line plus rows. */
export function PageSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div role="status" aria-label="Loading" className="flex flex-col gap-4 p-6">
      <Skeleton className="h-5 w-48" />
      <Skeleton className="h-3 w-80 max-w-full" />
      <div className="mt-2 overflow-hidden rounded-lg border border-border">
        {Array.from({ length: rows }, (_, i) => (
          <div key={i} className="flex items-center gap-3 border-b border-border px-4 py-3 last:border-0">
            <Skeleton className="size-4 rounded-full" />
            <Skeleton className={cx('h-3', ['w-1/2', 'w-2/3', 'w-2/5', 'w-3/5'][i % 4])} />
            <span className="flex-1" />
            <Skeleton className="h-3 w-16" />
          </div>
        ))}
      </div>
    </div>
  )
}

export function EmptyState({
  title,
  children,
  icon,
  action,
  size = 'page',
  className,
}: {
  title: ReactNode
  children?: ReactNode
  icon?: ReactNode
  action?: ReactNode
  size?: 'page' | 'inline'
  className?: string
}) {
  return (
    <div className={cx('mx-auto flex max-w-sm flex-col items-center text-center', size === 'page' ? 'px-4 py-20' : 'px-4 py-10', className)}>
      {icon && (
        <div
          className={cx(
            'mb-3 flex items-center justify-center rounded-xl border border-border bg-surface-1 text-fg-muted shadow-card',
            size === 'page' ? 'size-11' : 'size-9',
          )}
        >
          {icon}
        </div>
      )}
      <p className={cx('font-semibold', size === 'page' ? 'text-lg' : 'text-base')}>{title}</p>
      {children && <div className="mt-1 text-fg-muted">{children}</div>}
      {action && <div className="mt-4 flex flex-wrap justify-center gap-2">{action}</div>}
    </div>
  )
}
