import { Link } from '@tanstack/react-router'
import {
  CheckCircle2,
  CircleSlash,
  Eye,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  GitPullRequestClosed,
  History,
  Milestone,
  Pencil,
  RefreshCw,
  Tag,
  UserPlus,
  UserMinus,
  XCircle,
} from 'lucide-react'
import type { ComponentType } from 'react'
import type { PullEvent } from '../../api/client'
import { issueEventText } from '../../lib/issues'
import { eventText } from '../../lib/pulls'
import { cx, Time } from '../ui'
import { LabelChip } from './meta'

type Icon = ComponentType<{ size?: number; className?: string }>

const icons: Partial<Record<PullEvent['type'], Icon>> = {
  pushed: GitCommitHorizontal,
  merged: GitMerge,
  closed: GitPullRequestClosed,
  reopened: RefreshCw,
  base_changed: GitBranch,
  title_changed: Pencil,
  head_deleted: GitBranch,
  head_restored: GitBranch,
  branch_deleted: GitBranch,
  branch_updated: RefreshCw,
  review_requested: Eye,
  review_request_removed: Eye,
  review_dismissed: XCircle,
  ready_for_review: Eye,
  converted_to_draft: Tag,
  labeled: Tag,
  unlabeled: Tag,
  milestoned: Milestone,
  demilestoned: Milestone,
  assigned: UserPlus,
  unassigned: UserMinus,
}

/** One-line timeline event for issues and pull requests. */
export function EventLine({ repo, event, isIssue }: { repo: string; event: PullEvent; isIssue?: boolean }) {
  const d = event.data as Record<string, unknown>
  let Icon = icons[event.type] ?? History
  let tone = event.type === 'merged' ? 'text-done' : event.type === 'closed' ? 'text-danger' : 'text-fg-faint'
  if (isIssue && event.type === 'closed') {
    Icon = d.reason === 'not_planned' ? CircleSlash : CheckCircle2
    tone = d.reason === 'not_planned' ? 'text-fg-faint' : 'text-done'
  }
  const text = issueEventText(event) ?? eventText(event)
  return (
    <div className="flex items-center gap-2 px-3 text-sm text-fg-muted" data-testid="event">
      <Icon size={14} className={cx('shrink-0', tone)} />
      <span className="flex min-w-0 flex-wrap items-center gap-1">
        <span className="font-medium text-fg">{event.actor?.display_name ?? 'Someone'}</span> {text}
        {(event.type === 'labeled' || event.type === 'unlabeled') && <LabelChip label={{ name: String(d.label ?? ''), color: String(d.color ?? 'cfd3d7') }} />}
        {event.type === 'closed' && d.pull != null && (
          <Link to="/$repo/pulls/$number" params={{ repo, number: String(d.pull) }} className="font-medium text-accent hover:underline">
            #{String(d.pull)}
          </Link>
        )}
        {event.type === 'closed' && d.pull == null && typeof d.commit === 'string' && d.commit && (
          <Link to="/$repo/commit/$sha" params={{ repo, sha: d.commit }} className="font-mono text-accent hover:underline">
            {d.commit.slice(0, 7)}
          </Link>
        )}{' '}
        <span className="text-fg-faint">
          <Time iso={event.created_at} />
        </span>
      </span>
    </div>
  )
}
