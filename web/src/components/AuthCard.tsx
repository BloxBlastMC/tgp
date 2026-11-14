import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { metaQuery } from '../api/queries'
import { cx } from './ui'

/** Centered card used by setup, login and password pages. */
export function AuthCard({ title, subtitle, children, wide, aside }: { title: string; subtitle?: ReactNode; children: ReactNode; wide?: boolean; aside?: ReactNode }) {
  const meta = useQuery(metaQuery)
  return (
    <div className="relative flex min-h-full flex-col items-center overflow-auto bg-bg px-4">
      {/* Soft accent glow and a faint grid behind the card. */}
      <div aria-hidden className="pointer-events-none absolute inset-0 overflow-hidden">
        <div className="absolute -top-48 left-1/2 h-[480px] w-[880px] -translate-x-1/2 rounded-full bg-accent/12 blur-3xl" />
        <div className="absolute inset-0 [background-image:linear-gradient(var(--border)_1px,transparent_1px),linear-gradient(90deg,var(--border)_1px,transparent_1px)] [background-size:48px_48px] [mask-image:radial-gradient(ellipse_at_top,black_20%,transparent_70%)] opacity-60" />
      </div>
      <div className={cx('relative w-full py-[9vh]', wide ? 'max-w-xl' : 'max-w-sm')}>
        <div className="mb-7 flex flex-col items-center gap-3 text-center">
          <img src={meta.data?.has_logo ? '/api/v1/org/logo' : '/favicon.svg'} alt="" className="size-11 rounded-xl object-cover shadow-card" />
          <span className="text-lg font-semibold tracking-tight">{meta.data?.org_name || 'tgp'}</span>
        </div>
        <div className="rounded-xl border border-border bg-surface-1 p-7 shadow-dialog">
          <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
          {subtitle && <p className="mt-1 text-fg-muted">{subtitle}</p>}
          <div className="mt-6">{children}</div>
        </div>
        {aside && <div className="mt-4 text-center text-sm text-fg-muted">{aside}</div>}
        <p className="mt-8 text-center text-xs text-fg-faint">Powered by tgp {meta.data?.version ?? ''}</p>
      </div>
    </div>
  )
}
