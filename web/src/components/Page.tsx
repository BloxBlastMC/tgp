import type { ReactNode } from 'react'

/** Standard page frame: a slim header bar and a scrollable body. */
export function Page({ title, actions, children }: { title: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-1 flex-col">
      <header className="flex h-11 shrink-0 items-center justify-between border-b border-border px-4">
        <h1 className="font-medium">{title}</h1>
        <div className="flex items-center gap-2">{actions}</div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto">{children}</div>
    </div>
  )
}

export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="mx-auto mt-24 max-w-sm text-center">
      <p className="font-medium">{title}</p>
      {children && <div className="mt-1 text-fg-muted">{children}</div>}
    </div>
  )
}
