import { Link } from '@tanstack/react-router'
import { useId, useRef, type ComponentType, type ReactNode } from 'react'
import { Counter } from './Badge'
import { cx } from './cx'

export type TabItem<T extends string> = { value: T; label: ReactNode; count?: number; icon?: ReactNode }

/**
 * ARIA tabs (role=tablist/tab). Arrow keys move between tabs. Render the
 * panel yourself with <TabPanel> using the ids from tabIds().
 */
export function Tabs<T extends string>({
  value,
  onChange,
  items,
  label,
  idBase,
  className,
}: {
  value: T
  onChange: (v: T) => void
  items: TabItem<T>[]
  label: string
  idBase: string
  className?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  return (
    <div
      ref={ref}
      role="tablist"
      aria-label={label}
      className={cx('flex gap-1 border-b border-border', className)}
      onKeyDown={(e) => {
        const i = items.findIndex((t) => t.value === value)
        const next = e.key === 'ArrowRight' ? i + 1 : e.key === 'ArrowLeft' ? i - 1 : null
        if (next == null) return
        e.preventDefault()
        const t = items[(next + items.length) % items.length]!
        onChange(t.value)
        ref.current?.querySelector<HTMLElement>(`#${CSS.escape(`${idBase}-tab-${t.value}`)}`)?.focus()
      }}
    >
      {items.map((t) => {
        const on = t.value === value
        return (
          <button
            key={t.value}
            id={`${idBase}-tab-${t.value}`}
            type="button"
            role="tab"
            aria-selected={on}
            aria-controls={`${idBase}-panel-${t.value}`}
            tabIndex={on ? 0 : -1}
            onClick={() => onChange(t.value)}
            className={cx(
              '-mb-px flex h-8 items-center gap-1.5 border-b-2 px-2 text-sm font-medium transition-colors',
              on ? 'border-accent text-fg' : 'border-transparent text-fg-muted hover:text-fg',
            )}
          >
            {t.icon}
            {t.label}
            {t.count != null && <Counter>{t.count}</Counter>}
          </button>
        )
      })}
    </div>
  )
}

export function TabPanel({ idBase, value, children, className }: { idBase: string; value: string; children: ReactNode; className?: string }) {
  return (
    <div role="tabpanel" id={`${idBase}-panel-${value}`} aria-labelledby={`${idBase}-tab-${value}`} className={className}>
      {children}
    </div>
  )
}

export function useTabIds() {
  return useId().replace(/:/g, '')
}

export type NavTab = {
  to: string
  params?: Record<string, string>
  search?: Record<string, unknown>
  label: ReactNode
  icon?: ComponentType<{ size?: number; className?: string }>
  count?: number
  exact?: boolean
}

/** Underlined navigation tabs; each tab is a link. */
export function NavTabs({ items, className, label }: { items: NavTab[]; className?: string; label?: string }) {
  return (
    <nav aria-label={label} className={cx('-mb-px flex gap-0.5 overflow-x-auto', className)}>
      {items.map(({ to, params, search, label: text, icon: Icon, count, exact }) => (
        <Link
          key={to}
          to={to}
          params={params}
          search={search}
          activeOptions={{ exact, includeSearch: false }}
          className="group relative flex h-9 shrink-0 items-center gap-1.5 px-2.5 text-base text-fg-muted transition-colors hover:text-fg data-[status=active]:font-medium data-[status=active]:text-fg"
        >
          {Icon && <Icon size={14} className="text-fg-faint group-data-[status=active]:text-fg-muted" />}
          {text}
          {count != null && count > 0 && <Counter>{count}</Counter>}
          <span className="absolute inset-x-1.5 bottom-0 h-0.5 rounded-full bg-transparent group-hover:bg-border-strong group-data-[status=active]:bg-accent" />
        </Link>
      ))}
    </nav>
  )
}

/** A segmented toggle: buttons with aria-pressed. */
export function SegmentedControl<T extends string>({
  value,
  options,
  onChange,
  label,
  size = 'md',
  className,
}: {
  value: T
  options: { value: T; label: ReactNode; count?: number; icon?: ReactNode; title?: string }[]
  onChange: (v: T) => void
  label: string
  size?: 'sm' | 'md'
  className?: string
}) {
  return (
    <div className={cx('inline-flex shrink-0 rounded-md border border-border-strong bg-bg-subtle p-0.5', className)} role="group" aria-label={label}>
      {options.map((o) => {
        const on = value === o.value
        return (
          <button
            key={o.value}
            type="button"
            title={o.title}
            onClick={() => onChange(o.value)}
            aria-pressed={on}
            className={cx(
              'inline-flex items-center gap-1.5 rounded-[5px] font-medium whitespace-nowrap transition-colors',
              size === 'sm' ? 'h-5 px-2 text-xs' : 'h-6 px-2.5 text-sm',
              on ? 'bg-surface-2 text-fg shadow-[0_1px_2px_rgb(0_0_0/0.12),0_0_0_1px_var(--border-strong)]' : 'text-fg-muted hover:text-fg',
            )}
          >
            {o.icon}
            {o.label}
            {o.count != null && <span className={cx('tabular-nums', on ? 'text-fg-muted' : 'text-fg-faint')}>{o.count}</span>}
          </button>
        )
      })}
    </div>
  )
}
