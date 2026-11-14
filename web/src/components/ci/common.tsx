import { Link } from '@tanstack/react-router'
import { Ban, CheckCircle2, CircleDashed, CircleDot, Clock, GitBranch, GitCommit, GitPullRequest, Hourglass, Loader2, MinusCircle, Package, Play, Tag, Timer, XCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import type { WorkflowRun } from '../../api/client'
import { durationText, refLabel, statusKind, statusLabel, type StatusKind } from '../../lib/ci'
import { cx, Time } from '../ui'

const icons: Record<StatusKind, { icon: typeof CheckCircle2; className: string }> = {
  waiting: { icon: Hourglass, className: 'text-warning' },
  pending: { icon: CircleDashed, className: 'text-fg-faint' },
  queued: { icon: CircleDot, className: 'text-warning' },
  running: { icon: Loader2, className: 'text-accent animate-spin' },
  success: { icon: CheckCircle2, className: 'text-success' },
  failure: { icon: XCircle, className: 'text-danger' },
  cancelled: { icon: Ban, className: 'text-fg-muted' },
  skipped: { icon: MinusCircle, className: 'text-fg-faint' },
  timed_out: { icon: Timer, className: 'text-danger' },
}

/** Status icon for a run, job, step or check. */
export function StatusIcon({ status, conclusion, size = 15, className }: { status: string; conclusion?: string | null; size?: number; className?: string }) {
  const kind = statusKind(status, conclusion)
  return <KindIcon kind={kind} size={size} className={className} />
}

export function KindIcon({ kind, size = 15, className }: { kind: StatusKind; size?: number; className?: string }) {
  const { icon: Icon, className: c } = icons[kind]
  return (
    <span role="img" aria-label={statusLabel[kind]} title={statusLabel[kind]} className={cx('inline-flex shrink-0', className)}>
      <Icon size={size} className={c} />
    </span>
  )
}

/** Re-renders every second while `active`, for live durations. */
export function useTicker(active: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) return
    const t = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(t)
  }, [active])
  return now
}

export function Duration({ start, end }: { start?: string | null; end?: string | null }) {
  const now = useTicker(!!start && !end)
  const text = durationText(start, end, now)
  if (!text) return null
  return (
    <span className="inline-flex items-center gap-1 tabular-nums">
      <Clock size={11} /> {text}
    </span>
  )
}

export const eventLabel: Record<string, string> = { push: 'push', pull_request: 'pull request', workflow_dispatch: 'manual', release: 'release' }

export function EventBadge({ run }: { run: WorkflowRun }) {
  const Icon = run.event === 'pull_request' ? GitPullRequest : run.event === 'workflow_dispatch' ? Play : run.event === 'release' ? Package : run.ref.startsWith('refs/tags/') ? Tag : GitBranch
  return (
    <span className="inline-flex items-center gap-1" title={`${eventLabel[run.event] ?? run.event} · ${run.ref}`}>
      <Icon size={11} />
      <span className="max-w-40 truncate font-mono">{refLabel(run.ref, run.pull_number)}</span>
    </span>
  )
}

/** Rows of workflow runs; showRepo for cross-repository lists. */
export function RunRows({ runs, showRepo }: { runs: WorkflowRun[]; showRepo?: boolean }) {
  return (
    <ul>
      {runs.map((r) => (
        <li key={r.id} className="border-b border-border hover:bg-bg-subtle" data-testid="run-row">
          <Link to="/$repo/ci/runs/$number" params={{ repo: r.repo, number: String(r.number) }} search={{}} className="flex items-start gap-3 px-4 py-2.5">
            <StatusIcon status={r.status} conclusion={r.conclusion} className="mt-0.5" />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-x-2">
                {showRepo && <span className="text-fg-muted">{r.repo}</span>}
                <span className="truncate font-medium">{r.title || r.workflow_name}</span>
              </div>
              <div className="mt-0.5 flex flex-wrap items-center gap-x-2.5 text-[12px] text-fg-faint">
                <span className="text-fg-muted">{r.workflow_name}</span>
                <span>#{r.number}</span>
                <span>{eventLabel[r.event] ?? r.event}</span>
                <EventBadge run={r} />
                <span className="inline-flex items-center gap-1 font-mono">
                  <GitCommit size={11} />
                  {r.head_sha.slice(0, 7)}
                </span>
                {r.actor && <span>by {r.actor.display_name}</span>}
                {r.attempt > 1 && <span>attempt {r.attempt}</span>}
                {r.status_reason && <span className="max-w-80 truncate text-warning" title={r.status_reason}>{r.status_reason}</span>}
              </div>
            </div>
            <div className="flex shrink-0 flex-col items-end gap-0.5 text-[12px] text-fg-faint">
              <Time iso={r.created_at} />
              <Duration start={r.started_at} end={r.completed_at} />
            </div>
          </Link>
        </li>
      ))}
    </ul>
  )
}

/** Simple previous/next pagination from a total. */
export function Pager({ page, total, perPage, onPage }: { page: number; total: number; perPage: number; onPage: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / perPage))
  if (pages <= 1) return null
  return (
    <div className="flex items-center justify-center gap-3 py-3 text-[12px] text-fg-muted">
      <button type="button" disabled={page <= 1} onClick={() => onPage(page - 1)} className="rounded px-2 py-1 hover:bg-bg-hover disabled:opacity-40">
        ← Newer
      </button>
      <span>
        Page {page} of {pages}
      </span>
      <button type="button" disabled={page >= pages} onClick={() => onPage(page + 1)} className="rounded px-2 py-1 hover:bg-bg-hover disabled:opacity-40">
        Older →
      </button>
    </div>
  )
}
