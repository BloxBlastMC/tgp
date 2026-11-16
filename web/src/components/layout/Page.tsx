import { Link } from '@tanstack/react-router'
import { ArrowLeft, ChevronRight } from 'lucide-react'
import type { ComponentType, ReactNode } from 'react'
import { cx } from '../ui/cx'

export type Crumb = { label: ReactNode; to?: string; params?: Record<string, string>; search?: Record<string, unknown> }

const widths = {
  narrow: 'max-w-3xl',
  default: 'max-w-5xl',
  wide: 'max-w-7xl',
  full: '',
} as const

export type PageWidth = keyof typeof widths

/** Breadcrumb trail; the current page is plain text, never a link. */
export function Breadcrumbs({ items, className }: { items: Crumb[]; className?: string }) {
  if (items.length === 0) return null
  return (
    <nav aria-label="Breadcrumb" className={cx('min-w-0', className)}>
      <ol className="flex min-w-0 items-center gap-1 text-base text-fg-muted">
        {items.map((c, i) => (
          <li key={i} className="flex min-w-0 items-center gap-1">
            {c.to ? (
              <Link to={c.to} params={c.params} search={c.search} className="truncate rounded px-0.5 hover:text-fg">
                {c.label}
              </Link>
            ) : (
              <span className="truncate" aria-current={i === items.length - 1 ? 'page' : undefined}>
                {c.label}
              </span>
            )}
            {i < items.length - 1 && <ChevronRight size={13} className="shrink-0 text-fg-faint" aria-hidden />}
          </li>
        ))}
      </ol>
    </nav>
  )
}

/**
 * Standard page frame: a header bar (breadcrumbs, the page's h1, actions)
 * and a scrollable body. Give `width` to centre the body in a column.
 */
export function Page({
  title,
  crumbs,
  actions,
  children,
  width = 'full',
  description,
  icon,
  bodyClassName,
}: {
  title: ReactNode
  crumbs?: Crumb[]
  actions?: ReactNode
  children: ReactNode
  width?: PageWidth
  description?: ReactNode
  icon?: ComponentType<{ size?: number; className?: string }>
  bodyClassName?: string
}) {
  const Icon = icon
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <header className="flex h-12 shrink-0 items-center justify-between gap-4 border-b border-border px-5">
        <div className="flex min-w-0 items-center gap-2">
          {crumbs && crumbs.length > 0 && (
            <>
              <Breadcrumbs items={crumbs} className="hidden sm:block" />
              <ChevronRight size={13} className="hidden shrink-0 text-fg-faint sm:block" aria-hidden />
            </>
          )}
          {Icon && <Icon size={15} className="shrink-0 text-fg-muted" />}
          <h1 className="truncate text-base font-semibold">{title}</h1>
          {description && <span className="hidden truncate text-sm text-fg-faint md:inline">{description}</span>}
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </header>
      <div className={cx('min-h-0 flex-1 overflow-auto', bodyClassName)}>
        {width === 'full' ? children : <div className={cx('mx-auto w-full px-6 py-6', widths[width])}>{children}</div>}
      </div>
    </div>
  )
}

/** Centred content column inside a page body (for pages not using Page). */
export function Container({ width = 'default', children, className }: { width?: PageWidth; children: ReactNode; className?: string }) {
  return <div className={cx('mx-auto w-full px-6 py-6', widths[width], className)}>{children}</div>
}

/**
 * Title row inside a page body: optional back link, the heading, a
 * description and actions.
 */
export function SubHeader({
  back,
  title,
  description,
  actions,
  className,
  as: As = 'h1',
}: {
  back?: { to: string; params?: Record<string, string>; search?: Record<string, unknown>; label: string }
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
  as?: 'h1' | 'h2'
}) {
  return (
    <div className={cx('mb-5', className)}>
      {back && (
        <Link to={back.to} params={back.params} search={back.search} className="mb-2 inline-flex items-center gap-1 text-sm text-fg-muted hover:text-fg">
          <ArrowLeft size={13} /> {back.label}
        </Link>
      )}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <As className="text-xl font-semibold tracking-tight">{title}</As>
          {description && <p className="mt-0.5 text-fg-muted">{description}</p>}
        </div>
        {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}

export type SideNavItem = {
  to: string
  label: string
  params?: Record<string, string>
  icon?: ComponentType<{ size?: number; className?: string }>
  exact?: boolean
  hash?: string
}

/**
 * Section navigation for settings-style areas: a vertical list on wide
 * screens, a horizontally scrolling tab strip below `collapseBelow`.
 */
export function SideNav({
  label,
  groups,
  className,
  collapseBelow = 'lg',
}: {
  label: string
  groups: { title?: string; items: SideNavItem[] }[]
  className?: string
  collapseBelow?: 'lg' | 'xl'
}) {
  const link = ({ to, label: text, params, icon: Icon, exact, hash }: SideNavItem, strip: boolean) => (
    <Link
      key={to + (hash ?? '')}
      to={to}
      params={params}
      hash={hash}
      activeOptions={{ exact: exact ?? true, includeHash: !!hash, includeSearch: false }}
      className={cx(
        'group flex h-7 shrink-0 items-center gap-2 rounded-md px-2 text-base whitespace-nowrap text-fg-muted transition-colors hover:bg-bg-hover hover:text-fg data-[status=active]:bg-bg-active data-[status=active]:font-medium data-[status=active]:text-fg',
        strip && 'snap-start',
      )}
    >
      {Icon && <Icon size={14} className="shrink-0 text-fg-faint group-data-[status=active]:text-fg-muted" />}
      {text}
    </Link>
  )
  const wide = collapseBelow === 'xl' ? 'xl' : 'lg'
  return (
    <>
      <nav aria-label={label} className={cx('-mx-1 flex snap-x gap-1 overflow-x-auto px-1 pb-1', wide === 'xl' ? 'xl:hidden' : 'lg:hidden', className)}>
        {groups.flatMap((g) => g.items).map((it) => link(it, true))}
      </nav>
      <nav aria-label={label} className={cx('hidden flex-col gap-4', wide === 'xl' ? 'xl:flex' : 'lg:flex', className)}>
        {groups.map((g, gi) => (
          <div key={gi} className="flex flex-col gap-0.5">
            {g.title && <div className="mb-1 px-2 text-xs font-semibold tracking-wide text-fg-faint uppercase">{g.title}</div>}
            {g.items.map((it) => link(it, false))}
          </div>
        ))}
      </nav>
    </>
  )
}

/** A settings-style layout: section nav on the left, content on the right. */
export function TwoColumn({ nav, children, className }: { nav: ReactNode; children: ReactNode; className?: string }) {
  return (
    <div className={cx('mx-auto flex w-full max-w-7xl flex-col gap-4 px-6 py-6 lg:flex-row lg:gap-10', className)}>
      <div className="min-w-0 shrink-0 lg:sticky lg:top-6 lg:w-48 lg:self-start">{nav}</div>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}
