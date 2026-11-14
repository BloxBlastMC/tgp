import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { toneText, type Tone } from './Badge'
import { cx } from './cx'

/** A headline number with a label; a link when `to` is set. */
export function StatCard({
  label,
  value,
  icon,
  hint,
  to,
  search,
  tone,
  className,
}: {
  label: ReactNode
  value: ReactNode
  icon?: ReactNode
  hint?: ReactNode
  to?: string
  search?: Record<string, unknown>
  tone?: Tone
  className?: string
}) {
  const body = (
    <>
      <div className="flex items-center justify-between gap-2 text-sm text-fg-muted">
        <span className="truncate">{label}</span>
        {icon && <span className={cx('shrink-0', tone ? toneText[tone] : 'text-fg-faint')}>{icon}</span>}
      </div>
      <div className="mt-1.5 text-2xl font-semibold tracking-tight tabular-nums">{value}</div>
      {hint && <div className="mt-0.5 truncate text-sm text-fg-faint">{hint}</div>}
    </>
  )
  const cls = cx('block rounded-lg border border-border bg-surface-1 px-4 py-3 shadow-card', className)
  if (to)
    return (
      <Link to={to} search={search} className={cx(cls, 'transition-colors hover:border-border-strong hover:bg-bg-hover/50')}>
        {body}
      </Link>
    )
  return <div className={cls}>{body}</div>
}

/** Tone for a usage percentage: warn from 75%, danger from 90%. */
export function meterTone(pct: number): Tone {
  return pct >= 90 ? 'danger' : pct >= 75 ? 'warning' : 'accent'
}

const fill: Partial<Record<Tone, string>> = {
  accent: 'bg-accent-solid',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
  info: 'bg-info',
  done: 'bg-done',
  neutral: 'bg-fg-faint',
}

/** A horizontal usage/progress bar (role=meter). */
export function Meter({
  value,
  max = 100,
  label,
  tone = 'auto',
  size = 'md',
  className,
}: {
  value: number
  max?: number
  label: string
  tone?: Tone | 'auto'
  size?: 'sm' | 'md'
  className?: string
}) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0
  const t = tone === 'auto' ? meterTone(pct) : tone
  return (
    <div
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={value}
      className={cx('w-full overflow-hidden rounded-full bg-bg-active', size === 'sm' ? 'h-1' : 'h-1.5', className)}
    >
      <div className={cx('h-full rounded-full transition-[width] duration-300', fill[t])} style={{ width: `${pct}%` }} />
    </div>
  )
}

/** Term/detail pairs, as a two-column grid or stacked. */
export function DescriptionList({
  items,
  layout = 'grid',
  className,
  termWidth = 'sm:grid-cols-[140px_minmax(0,1fr)]',
}: {
  items: { term: ReactNode; detail: ReactNode; key?: string }[]
  layout?: 'grid' | 'stacked'
  className?: string
  termWidth?: string
}) {
  return (
    <dl className={cx(layout === 'grid' ? cx('grid grid-cols-1 gap-x-4 gap-y-2.5', termWidth) : 'flex flex-col gap-3', className)}>
      {items.map((it, i) => (
        <div key={it.key ?? i} className={layout === 'grid' ? 'contents' : ''}>
          <dt className="text-fg-muted">{it.term}</dt>
          <dd className="min-w-0 break-words">{it.detail}</dd>
        </div>
      ))}
    </dl>
  )
}
