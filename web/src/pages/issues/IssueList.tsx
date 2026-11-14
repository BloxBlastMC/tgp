import { Link } from '@tanstack/react-router'
import { MessageSquare } from 'lucide-react'
import type { IssueSummary } from '../../api/client'
import { IssueStateIcon, LabelChip, MilestoneLink } from '../../components/issues/meta'
import { Avatar } from '../../components/pulls/common'
import { Time } from '../../components/ui'

/** Rows of issues; showRepo for cross-repository lists. onLabel filters by a label. */
export function IssueRows({ issues, showRepo, onLabel }: { issues: IssueSummary[]; showRepo?: boolean; onLabel?: (name: string) => void }) {
  return (
    <ul>
      {issues.map((i) => (
        <li key={`${i.repo}#${i.number}`} className="flex items-start gap-3 border-b border-border px-4 py-2.5 hover:bg-bg-subtle" data-testid="issue-row">
          <span className="mt-0.5">
            <IssueStateIcon issue={i} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              {showRepo && <span className="text-fg-muted">{i.repo}</span>}
              <Link to="/$repo/issues/$number" params={{ repo: i.repo, number: String(i.number) }} className="font-medium hover:text-accent">
                {i.title}
              </Link>
              {i.labels.map((l) => (
                <LabelChip key={l.id} label={l} onClick={onLabel ? () => onLabel(l.name) : undefined} title={onLabel ? `Filter by ${l.name}` : l.description} />
              ))}
            </div>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-sm text-fg-faint">
              <span>#{i.number}</span>
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
              {i.milestone && <MilestoneLink repo={i.repo} milestone={i.milestone} />}
            </div>
          </div>
          {i.assignees.length > 0 && (
            <span className="mt-0.5 flex shrink-0 -space-x-1" title={`Assigned to ${i.assignees.map((a) => a.display_name).join(', ')}`}>
              {i.assignees.slice(0, 3).map((a) => (
                <Avatar key={a.id} user={a} size={18} />
              ))}
            </span>
          )}
          <span className="mt-0.5 flex w-10 shrink-0 items-center justify-end gap-1 text-sm text-fg-muted" title={`${i.comments} comments`}>
            {i.comments > 0 && (
              <>
                <MessageSquare size={13} /> {i.comments}
              </>
            )}
          </span>
        </li>
      ))}
    </ul>
  )
}
