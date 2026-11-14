import { Link } from '@tanstack/react-router'
import { MessageSquare, Milestone } from 'lucide-react'
import type { PullSummary } from '../../api/client'
import type { PullState } from '../../api/queries'
import { LabelChip } from '../../components/issues/meta'
import { BranchName, DecisionBadge, PullStateIcon } from '../../components/pulls/common'
import { AvatarStack, Badge, SegmentedControl, Sep, Time } from '../../components/ui'

export const pullStates: { value: PullState; label: string }[] = [
  { value: 'open', label: 'Open' },
  { value: 'merged', label: 'Merged' },
  { value: 'closed', label: 'Closed' },
  { value: 'all', label: 'All' },
]

export type PullListSearch = { state?: PullState; q?: string; page?: number }

export function validatePullList(s: Record<string, unknown>): PullListSearch {
  const out: PullListSearch = {}
  if (typeof s.state === 'string' && ['open', 'closed', 'merged', 'all'].includes(s.state)) out.state = s.state as PullState
  if (typeof s.q === 'string' && s.q) out.q = s.q
  const p = Number(s.page)
  if (Number.isInteger(p) && p > 1) out.page = p
  return out
}

/** @deprecated use SegmentedControl */
export const Segmented = SegmentedControl

/** Rows of pull requests; showRepo for cross-repository lists. */
export function PullRows({ pulls, showRepo }: { pulls: PullSummary[]; showRepo?: boolean }) {
  return (
    <ul>
      {pulls.map((p) => (
        <li key={`${p.repo}#${p.number}`} className="relative flex items-start gap-3 border-b border-border px-5 py-3 transition-colors hover:bg-bg-hover/50">
          <span className="mt-0.5">
            <PullStateIcon pr={p} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              {showRepo && <span className="text-fg-muted">{p.repo}</span>}
              <Link
                to="/$repo/pulls/$number"
                params={{ repo: p.repo, number: String(p.number) }}
                className="font-semibold after:absolute after:inset-0 hover:text-accent"
              >
                {p.title}
              </Link>
              {p.draft && p.state === 'open' && <Badge>draft</Badge>}
              <DecisionBadge decision={p.review_decision} />
              {p.labels.map((l) => (
                <span key={l.id} className="relative z-[1]">
                  <LabelChip label={l} />
                </span>
              ))}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-1.5 text-sm text-fg-muted">
              <span className="tabular-nums">#{p.number}</span>
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
              <Sep />
              <span className="flex items-center gap-1">
                <BranchName>{p.head_branch}</BranchName> → <BranchName>{p.base_branch}</BranchName>
              </span>
              {p.milestone && (
                <>
                  <Sep />
                  <span className="inline-flex items-center gap-1">
                    <Milestone size={12} /> {p.milestone.title}
                  </span>
                </>
              )}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-3 self-center">
            {p.requested_reviewers.length > 0 && (
              <span className="hidden items-center gap-1.5 text-sm text-fg-faint md:flex" title={`Review requested from ${p.requested_reviewers.map((u) => u.display_name).join(', ')}`}>
                <AvatarStack users={p.requested_reviewers} size={18} />
              </span>
            )}
            {p.comments > 0 && (
              <span className="flex w-10 items-center justify-end gap-1 text-sm text-fg-muted tabular-nums" title={`${p.comments} comments`}>
                <MessageSquare size={13} /> {p.comments}
              </span>
            )}
          </div>
        </li>
      ))}
    </ul>
  )
}
