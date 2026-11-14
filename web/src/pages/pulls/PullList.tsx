import { Link } from '@tanstack/react-router'
import { MessageSquare } from 'lucide-react'
import type { PullSummary } from '../../api/client'
import { BranchName, DecisionBadge, PullStateIcon } from '../../components/pulls/common'
import { Badge, cx, Time } from '../../components/ui'
import type { PullState } from '../../api/queries'

export const pullStates: { value: PullState; label: string }[] = [
  { value: 'open', label: 'Open' },
  { value: 'merged', label: 'Merged' },
  { value: 'closed', label: 'Closed' },
  { value: 'all', label: 'All' },
]

export type PullListSearch = { state?: PullState; q?: string }

export function validatePullList(s: Record<string, unknown>): PullListSearch {
  const out: PullListSearch = {}
  if (typeof s.state === 'string' && ['open', 'closed', 'merged', 'all'].includes(s.state)) out.state = s.state as PullState
  if (typeof s.q === 'string' && s.q) out.q = s.q
  return out
}

export function Segmented<T extends string>({ value, options, onChange, label }: { value: T; options: { value: T; label: string }[]; onChange: (v: T) => void; label: string }) {
  return (
    <div className="flex rounded border border-border-strong" role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          aria-pressed={value === o.value}
          className={cx('h-7 px-2.5 text-[12px]', value === o.value ? 'bg-bg-hover text-fg' : 'text-fg-muted hover:text-fg')}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/** Rows of pull requests; showRepo for cross-repository lists. */
export function PullRows({ pulls, showRepo }: { pulls: PullSummary[]; showRepo?: boolean }) {
  return (
    <ul>
      {pulls.map((p) => (
        <li key={`${p.repo}#${p.number}`} className="border-b border-border hover:bg-bg-subtle">
          <Link to="/$repo/pulls/$number" params={{ repo: p.repo, number: String(p.number) }} className="flex items-start gap-3 px-4 py-2.5">
            <span className="mt-0.5">
              <PullStateIcon pr={p} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                {showRepo && <span className="text-fg-muted">{p.repo}</span>}
                <span className="font-medium">{p.title}</span>
                {p.draft && p.state === 'open' && <Badge>draft</Badge>}
                <DecisionBadge decision={p.review_decision} />
              </div>
              <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-[12px] text-fg-faint">
                <span>#{p.number}</span>
                <span>
                  {p.state === 'merged' ? (
                    <>
                      merged <Time iso={p.merged_at} />
                    </>
                  ) : p.state === 'closed' ? (
                    <>
                      closed <Time iso={p.closed_at} />
                    </>
                  ) : (
                    <>
                      opened <Time iso={p.created_at} />
                    </>
                  )}{' '}
                  by {p.author.display_name}
                </span>
                <span className="flex items-center gap-1">
                  <BranchName>{p.head_branch}</BranchName> → <BranchName>{p.base_branch}</BranchName>
                </span>
                {p.requested_reviewers.length > 0 && <span>review requested from {p.requested_reviewers.map((u) => u.display_name).join(', ')}</span>}
              </div>
            </div>
            {p.comments > 0 && (
              <span className="mt-0.5 flex shrink-0 items-center gap-1 text-[12px] text-fg-muted" title={`${p.comments} comments`}>
                <MessageSquare size={13} /> {p.comments}
              </span>
            )}
          </Link>
        </li>
      ))}
    </ul>
  )
}
