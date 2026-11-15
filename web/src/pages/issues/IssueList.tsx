import { Link } from '@tanstack/react-router'
import { MessageSquare } from 'lucide-react'
import type { IssueSummary } from '../../api/client'
import { IssueStateIcon, LabelChip, MilestoneLink } from '../../components/issues/meta'
import { AvatarStack, Sep, Time } from '../../components/ui'

/** Rows of issues; showRepo for cross-repository lists. onLabel filters by a label. */
export function IssueRows({ issues, showRepo, onLabel }: { issues: IssueSummary[]; showRepo?: boolean; onLabel?: (name: string) => void }) {
  return (
    <ul>
      {issues.map((i) => (
        <li key={`${i.repo}#${i.number}`} className="relative flex items-start gap-3 border-b border-border px-5 py-3 transition-colors hover:bg-bg-hover has-[a:focus-visible]:outline-2 has-[a:focus-visible]:-outline-offset-2 has-[a:focus-visible]:outline-accent" data-testid="issue-row">
          <span className="mt-0.5">
            <IssueStateIcon issue={i} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              {showRepo && <span className="text-fg-muted">{i.repo}</span>}
              <Link to="/$repo/issues/$number" params={{ repo: i.repo, number: String(i.number) }} className="font-semibold after:absolute after:inset-0 hover:text-accent">
                {i.title}
              </Link>
              {i.labels.map((l) => (
                <span key={l.id} className="relative z-[1]">
                  <LabelChip label={l} onClick={onLabel ? () => onLabel(l.name) : undefined} title={onLabel ? `Filter by ${l.name}` : l.description} />
                </span>
              ))}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-1.5 text-sm text-fg-muted">
              <span className="tabular-nums">#{i.number}</span>
              <span>
                {i.state === 'closed' ? (
                  <>
                    closed <Time iso={i.closed_at} />
                  </>
                ) : (
                  <>
                    opened <Time iso={i.created_at} />
                  </>
                )}{' '}
                by {i.author.display_name}
              </span>
              {i.milestone && (
                <>
                  <Sep />
                  <span className="relative z-[1]">
                    <MilestoneLink repo={i.repo} milestone={i.milestone} />
                  </span>
                </>
              )}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-3 self-center">
            {i.assignees.length > 0 && (
              <span title={`Assigned to ${i.assignees.map((a) => a.display_name).join(', ')}`}>
                <AvatarStack users={i.assignees} size={20} />
              </span>
            )}
            <span className="flex w-10 items-center justify-end gap-1 text-sm text-fg-muted tabular-nums" title={`${i.comments} comments`}>
              {i.comments > 0 && (
                <>
                  <MessageSquare size={13} /> {i.comments}
                </>
              )}
            </span>
          </div>
        </li>
      ))}
    </ul>
  )
}
