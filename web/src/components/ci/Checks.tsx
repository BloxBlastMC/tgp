import { Link } from '@tanstack/react-router'
import { ChevronDown, ChevronRight, CircleDashed } from 'lucide-react'
import { useState } from 'react'
import type { CheckSummary, Schemas } from '../../api/client'
import { checksHeadline } from '../../lib/ci'
import { Badge } from '../ui'
import { Duration, KindIcon, StatusIcon } from './common'

/** The checks row of a pull request's merge box. */
export function ChecksRow({ repo, checks, blocker }: { repo: string; checks: CheckSummary; blocker?: Schemas['MergeBlocker'] }) {
  const [open, setOpen] = useState(checks.state !== 'success')
  if (checks.total === 0 && checks.missing_required.length === 0) return null
  const kind = checks.state === 'failure' ? 'failure' : checks.state === 'pending' ? 'running' : 'success'
  return (
    <li className="py-1.5" aria-label="Checks">
      <button type="button" onClick={() => setOpen((o) => !o)} className="flex w-full items-start gap-2 text-left" aria-expanded={open}>
        <KindIcon kind={kind} className="mt-0.5" />
        <span className="min-w-0 flex-1">
          {checksHeadline(checks)}
          {blocker && (
            <span className="block text-sm text-fg-muted">
              {blocker.message}
              {blocker.bypassable && <span className="text-fg-faint"> (admins may bypass)</span>}
            </span>
          )}
        </span>
        {open ? <ChevronDown size={14} className="mt-0.5 text-fg-faint" /> : <ChevronRight size={14} className="mt-0.5 text-fg-faint" />}
      </button>
      {open && (
        <ul className="mt-1.5 ml-6 flex flex-col overflow-hidden rounded-md border border-border bg-surface-1" aria-label="Check list">
          {checks.checks.map((c) => (
            <li key={c.job_id} className="flex items-center gap-2 border-b border-border px-2.5 py-1.5 last:border-b-0">
              <StatusIcon status={c.status} conclusion={c.conclusion} size={14} />
              <span className="min-w-0 flex-1 truncate">
                <span className="text-fg-muted">{c.workflow} / </span>
                <span className="font-medium">{c.job_name}</span>
              </span>
              {c.required && <Badge tone="accent">Required</Badge>}
              <span className="text-xs text-fg-faint">
                <Duration start={c.started_at} end={c.completed_at} />
              </span>
              <Link to="/$repo/ci/runs/$number" params={{ repo, number: String(c.run_number) }} search={{ job: c.job_id }} className="text-sm text-accent hover:underline">
                Details
              </Link>
            </li>
          ))}
          {checks.missing_required.map((name) => (
            <li key={`missing:${name}`} className="flex items-center gap-2 border-b border-border px-2.5 py-1.5 last:border-b-0">
              <CircleDashed size={14} className="shrink-0 text-warning" />
              <span className="min-w-0 flex-1 truncate font-medium">{name}</span>
              <Badge tone="accent">Required</Badge>
              <span className="text-sm text-fg-faint">Expected — waiting for status</span>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}
