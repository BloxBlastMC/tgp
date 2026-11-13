import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { metaQuery } from '../api/queries'

/** Centered card used by setup, login and password pages. */
export function AuthCard({ title, subtitle, children, wide }: { title: string; subtitle?: ReactNode; children: ReactNode; wide?: boolean }) {
  const meta = useQuery(metaQuery)
  return (
    <div className="flex min-h-full items-start justify-center overflow-auto bg-bg px-4 py-[10vh]">
      <div className={wide ? 'w-full max-w-xl' : 'w-full max-w-sm'}>
        <div className="mb-6 flex items-center gap-2.5">
          <img src={meta.data?.has_logo ? '/api/v1/org/logo' : '/favicon.svg'} alt="" className="size-7 rounded object-cover" />
          <span className="text-[15px] font-semibold">{meta.data?.org_name || 'tgp'}</span>
        </div>
        <div className="rounded-lg border border-border bg-bg-subtle p-6">
          <h1 className="text-[17px] font-semibold">{title}</h1>
          {subtitle && <p className="mt-1 text-fg-muted">{subtitle}</p>}
          <div className="mt-5">{children}</div>
        </div>
      </div>
    </div>
  )
}
