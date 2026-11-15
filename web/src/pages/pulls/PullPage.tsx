import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, Navigate, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import {
  Check,
  CheckCircle2,
  Eye,
  FileDiff,
  GitPullRequest,
  GitPullRequestClosed,
  History,
  MessageSquare,
  Pencil,
  Shield,
  XCircle,
} from 'lucide-react'
import { useMemo, useState, type ComponentType, type ReactNode } from 'react'
import { api, ApiRequestError, errorMessage, unwrap, type Comment, type PullRequest, type Review } from '../../api/client'
import { pullCommentsQuery, pullCommitsQuery, pullFilesQuery, pullQuery, pullTimelineQuery, sessionQuery } from '../../api/queries'
import { DiffView, type DiffAnnotations } from '../../components/code/DiffView'
import { CommitList } from '../../components/code/CommitList'
import { EmptyState } from '../../components/Page'
import {
  Avatar,
  BranchName,
  CommentCard,
  MarkdownEditor,
  PullStateBadge,
  ReviewStateIcon,
  ThreadView,
  useInvalidatePull,
} from '../../components/pulls/common'
import { ReviewerPicker } from '../../components/pulls/ReviewerPicker'
import { Alert, Badge, Button, CopyButton, Counter, cx, Dialog, ErrorState, IconButton, Input, MetaSection, PageSkeleton, RadioCard, SkeletonRows, Time, Tooltip, useToast } from '../../components/ui'
import { DiffStat } from '../../components/code/DiffView'
import { canTriage as canTriagePerm, canWrite as canWritePerm, isRepoAdmin } from '../../lib/permissions'
import { anchorThreads, groupThreads, lineKey, type Thread } from '../../lib/pulls'
import { EventLine } from '../../components/issues/EventLine'
import { MetaSections, type MetaPatch } from '../../components/issues/meta'
import { useRepoName } from '../repos/RepoLayout'
import { MergeBox } from './MergeBox'

type Tab = 'conversation' | 'commits' | 'files'
export type PullSearch = { tab?: Tab }

export function validatePull(s: Record<string, unknown>): PullSearch {
  return s.tab === 'commits' || s.tab === 'files' ? { tab: s.tab } : {}
}

/** An inline comment waiting to be submitted with a review. */
type Pending = { key: string; path: string; side: 'old' | 'new'; line: number; body: string }

export function PullPage() {
  const repo = useRepoName()
  const { number: numStr } = useParams({ strict: false }) as { number: string }
  const number = Number(numStr)
  const search = useSearch({ strict: false }) as PullSearch
  const tab = search.tab ?? 'conversation'
  const pr = useQuery({ ...pullQuery(repo, number), enabled: Number.isInteger(number) && number > 0, refetchInterval: (q) => (q.state.data?.state === 'open' && q.state.data.checks?.state === 'pending' ? 5000 : false) })
  const [pending, setPending] = useState<Pending[]>([])
  const [reviewOpen, setReviewOpen] = useState(false)

  if (!Number.isInteger(number) || number <= 0) return <EmptyState icon={<GitPullRequest size={20} />} title="Pull request not found" />
  if (pr.isPending) return <PageSkeleton rows={3} />
  if (pr.isError) {
    if (pr.error instanceof ApiRequestError && pr.error.status === 404 && pr.error.details?.is_issue)
      return <Navigate to="/$repo/issues/$number" params={{ repo, number: String(number) }} replace />
    if (pr.error instanceof ApiRequestError && pr.error.status === 404)
      return (
        <EmptyState icon={<GitPullRequest size={20} />} title="Pull request not found">
          #{number} doesn't exist in {repo}.
        </EmptyState>
      )
    return <ErrorState error={pr.error} />
  }
  const p = pr.data
  return (
    <div className="flex flex-col">
      <PullHeader repo={repo} pr={p} tab={tab} pendingCount={pending.length} onReview={() => setReviewOpen(true)} />
      {tab === 'conversation' && <Conversation repo={repo} pr={p} />}
      {tab === 'commits' && <CommitsTab repo={repo} pr={p} />}
      {tab === 'files' && <FilesTab repo={repo} pr={p} pending={pending} setPending={setPending} />}
      <ReviewDialog repo={repo} pr={p} open={reviewOpen} onClose={() => setReviewOpen(false)} pending={pending} onSubmitted={() => setPending([])} />
    </div>
  )
}

function useViewer(pr: PullRequest) {
  const me = useQuery(sessionQuery).data!.user
  const perm = pr.viewer_permission
  const canWrite = canWritePerm(perm)
  const canTriage = canTriagePerm(perm)
  const isAuthor = me.id === pr.author.id
  return { me, canWrite, canTriage, isAuthor, canEdit: canWrite || (canTriage && isAuthor), isRepoAdmin: isRepoAdmin(perm) }
}

function PullHeader({ repo, pr, tab, pendingCount, onReview }: { repo: string; pr: PullRequest; tab: Tab; pendingCount: number; onReview: () => void }) {
  const v = useViewer(pr)
  const invalidate = useInvalidatePull(repo, pr.number)
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(pr.title)
  const save = useMutation({
    mutationFn: () => unwrap(api.PATCH('/repos/{repo}/pulls/{number}', { params: { path: { repo, number: pr.number } }, body: { title } })),
    onSuccess: () => {
      setEditing(false)
      invalidate()
    },
  })
  const tabs: { id: Tab; label: string; icon: ComponentType<{ size?: number; className?: string }>; count: number }[] = [
    { id: 'conversation', label: 'Conversation', icon: MessageSquare, count: pr.comments },
    { id: 'commits', label: 'Commits', icon: History, count: pr.commits },
    { id: 'files', label: 'Files changed', icon: FileDiff, count: pr.changed_files },
  ]
  return (
    <div className="border-b border-border">
      <div className="mx-auto w-full max-w-7xl px-6 pt-4">
      {editing ? (
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <Input value={title} onChange={(e) => setTitle(e.target.value)} aria-label="Title" autoFocus size="lg" className="text-lg font-semibold" />
          <Button type="submit" variant="primary" loading={save.isPending} disabled={!title.trim()}>
            Save
          </Button>
          <Button variant="ghost" onClick={() => setEditing(false)}>
            Cancel
          </Button>
        </form>
      ) : (
        <div className="flex items-start gap-2">
          <h1 className="min-w-0 flex-1 text-xl font-semibold tracking-tight">
            {pr.title} <span className="font-normal text-fg-faint">#{pr.number}</span>
          </h1>
          {v.canEdit && (
            <Button
              variant="ghost"
              icon={<Pencil size={13} />}
              onClick={() => {
                setTitle(pr.title)
                setEditing(true)
              }}
            >
              Edit
            </Button>
          )}
        </div>
      )}
      {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
      <div className="mt-2 flex flex-wrap items-center gap-2 text-fg-muted">
        <PullStateBadge pr={pr} />
        <span className="flex flex-wrap items-center gap-1">
          <Avatar user={pr.author} size={18} />
          <span className="font-medium text-fg">{pr.author.display_name}</span> {pr.state === 'merged' ? 'merged' : 'wants to merge'} {pr.commits} commit
          {pr.commits === 1 ? '' : 's'} into <BranchName>{pr.base.branch}</BranchName> from <BranchName>{pr.head.branch}</BranchName>
          <CopyButton value={pr.head.branch} label="Copy branch name" size="xs" />
          {!pr.head.exists && pr.state !== 'merged' && <span className="text-danger">(deleted)</span>}
        </span>
      </div>
      <div className="mt-3 -mb-px flex items-end gap-1">
        <nav className="flex gap-0.5" aria-label="Pull request">
          {tabs.map(({ id, label, icon: Icon, count }) => (
            <Link
              key={id}
              to="/$repo/pulls/$number"
              params={{ repo, number: String(pr.number) }}
              search={id === 'conversation' ? {} : { tab: id }}
              className={cx(
                'relative flex h-9 items-center gap-1.5 px-2.5 transition-colors',
                tab === id ? 'font-medium text-fg' : 'text-fg-muted hover:text-fg',
              )}
              aria-current={tab === id ? 'page' : undefined}
            >
              <Icon size={14} className="text-fg-faint" /> {label} <Counter>{count}</Counter>
              <span className={cx('absolute inset-x-1.5 bottom-0 h-0.5 rounded-full', tab === id ? 'bg-accent' : 'bg-transparent')} />
            </Link>
          ))}
        </nav>
        <div className="flex-1" />
        <span className="hidden pb-2.5 text-sm md:block">
          <DiffStat additions={pr.additions} deletions={pr.deletions} bar />
        </span>
        {pr.state === 'open' && v.canTriage && (
          <Button variant={pendingCount > 0 ? 'primary' : 'secondary'} className="mb-1.5 ml-2" onClick={onReview} icon={<Eye size={13} />}>
            Review{pendingCount > 0 && ` (${pendingCount} pending)`}
          </Button>
        )}
      </div>
    </div>
    </div>
  )
}

// ---- conversation ----

function Conversation({ repo, pr }: { repo: string; pr: PullRequest }) {
  const timeline = useQuery(pullTimelineQuery(repo, pr.number))
  const comments = useQuery(pullCommentsQuery(repo, pr.number))
  const v = useViewer(pr)
  const threads = useMemo(() => new Map(groupThreads(comments.data ?? []).map((t) => [t.root.id, t])), [comments.data])
  const threadOf = (c: Comment): Thread => threads.get(c.id) ?? { root: c, replies: [] }

  return (
    <div className="mx-auto grid w-full max-w-7xl gap-8 px-6 py-6 md:grid-cols-[minmax(0,1fr)_256px]">
      <div className="relative flex min-w-0 flex-col gap-4 before:absolute before:top-4 before:bottom-24 before:left-[19px] before:w-px before:bg-border [&>*]:relative">
        <BodyCard repo={repo} pr={pr} canEdit={v.canEdit} />
        {timeline.isPending ? (
          <SkeletonRows rows={3} />
        ) : timeline.isError ? (
          <Alert>{errorMessage(timeline.error)}</Alert>
        ) : (
          timeline.data.map((it, i) => {
            if (it.kind === 'event' && it.event) return <EventLine key={`e${it.event.id}`} repo={repo} event={it.event} />
            if (it.kind === 'review' && it.review)
              return <ReviewCard key={`r${it.review.id}`} repo={repo} pr={pr} review={it.review} threadOf={threadOf} viewer={v} />
            if (it.kind === 'inline_comment' && it.comment)
              return <ThreadView key={`t${it.comment.id}`} repo={repo} number={pr.number} thread={threadOf(it.comment)} canTriage={v.canTriage} isRepoAdmin={v.isRepoAdmin} showContext />
            if (it.comment) return <CommentCard key={`c${it.comment.id}`} repo={repo} number={pr.number} comment={it.comment} isRepoAdmin={v.isRepoAdmin} opAuthorId={pr.author.id} />
            return <span key={i} />
          })
        )}
        <MergeBox repo={repo} pr={pr} />
        {v.canTriage && <CommentComposer repo={repo} pr={pr} canEdit={v.canEdit} />}
      </div>
      <PullSidebar repo={repo} pr={pr} viewer={v} />
    </div>
  )
}

function BodyCard({ repo, pr, canEdit }: { repo: string; pr: PullRequest; canEdit: boolean }) {
  const invalidate = useInvalidatePull(repo, pr.number)
  const [editing, setEditing] = useState(false)
  const [body, setBody] = useState(pr.body)
  const save = useMutation({
    mutationFn: () => unwrap(api.PATCH('/repos/{repo}/pulls/{number}', { params: { path: { repo, number: pr.number } }, body: { body } })),
    onSuccess: () => {
      setEditing(false)
      invalidate()
    },
  })
  return (
    <div className="relative overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
      <div className="flex items-center gap-2 border-b border-border bg-bg-subtle px-3 py-1.5 text-sm text-fg-muted">
        <Avatar user={pr.author} size={18} />
        <span className="font-medium text-fg">{pr.author.display_name}</span> opened this <Time iso={pr.created_at} />
        <span className="flex-1" />
        {canEdit && !editing && (
          <IconButton
            label="Edit description"
            size="xs"
            onClick={() => {
              setBody(pr.body)
              setEditing(true)
            }}
          >
            <Pencil size={12} />
          </IconButton>
        )}
      </div>
      <div className="px-4 py-3">
        {editing ? (
          <div className="flex flex-col gap-2">
            <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Description" autoFocus minRows={6} />
            {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setEditing(false)}>
                Cancel
              </Button>
              <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
                Save
              </Button>
            </div>
          </div>
        ) : pr.body_html ? (
          <div className="markdown" dangerouslySetInnerHTML={{ __html: pr.body_html }} />
        ) : (
          <p className="text-fg-faint italic">No description.</p>
        )}
      </div>
    </div>
  )
}

const reviewVerb: Record<Review['state'], string> = {
  approved: 'approved these changes',
  changes_requested: 'requested changes',
  commented: 'reviewed',
}

function ReviewCard({
  repo,
  pr,
  review,
  threadOf,
  viewer,
}: {
  repo: string
  pr: PullRequest
  review: Review
  threadOf: (c: Comment) => Thread
  viewer: ReturnType<typeof useViewer>
}) {
  const invalidate = useInvalidatePull(repo, pr.number)
  const [dismissing, setDismissing] = useState(false)
  const [message, setMessage] = useState('')
  const dismiss = useMutation({
    mutationFn: () =>
      unwrap(api.POST('/repos/{repo}/pulls/{number}/reviews/{id}/dismiss', { params: { path: { repo, number: pr.number, id: review.id } }, body: { message } })),
    onSuccess: () => {
      setDismissing(false)
      invalidate()
    },
  })
  const border = review.dismissed ? 'border-border' : review.state === 'approved' ? 'border-success/40' : review.state === 'changes_requested' ? 'border-danger/40' : 'border-border'
  return (
    <div className={cx('relative overflow-hidden rounded-lg border bg-surface-1 shadow-card', border)} data-testid="review">
      <div className="flex flex-wrap items-center gap-2 bg-bg-subtle px-3 py-1.5 text-sm text-fg-muted">
        <Avatar user={review.author} size={18} />
        <ReviewStateIcon state={review.dismissed ? 'dismissed' : review.state} />
        <span className="font-medium text-fg">{review.author.display_name}</span> {reviewVerb[review.state]} <Time iso={review.created_at} />
        {review.stale && !review.dismissed && review.state !== 'commented' && <Badge tone="warning">outdated</Badge>}
        {review.dismissed && <Badge>dismissed</Badge>}
        <span className="flex-1" />
        {viewer.canWrite && pr.state === 'open' && !review.dismissed && review.state !== 'commented' && (
          <Button variant="ghost" size="xs" onClick={() => setDismissing(true)}>
            Dismiss
          </Button>
        )}
      </div>
      {(review.body_html || review.comments.length > 0 || review.dismissed) && (
        <div className="flex flex-col gap-2 border-t border-border px-4 py-3">
          {review.dismissed && review.dismiss_message && (
            <p className="text-sm text-fg-muted">
              Dismissed by {review.dismissed_by?.display_name}: {review.dismiss_message}
            </p>
          )}
          {review.body_html && <div className="markdown" dangerouslySetInnerHTML={{ __html: review.body_html }} />}
          {review.comments.map((c) => (
            <ThreadView key={c.id} repo={repo} number={pr.number} thread={threadOf(c)} canTriage={viewer.canTriage} isRepoAdmin={viewer.isRepoAdmin} showContext />
          ))}
        </div>
      )}
      <Dialog
        open={dismissing}
        onClose={() => setDismissing(false)}
        title={`Dismiss ${review.author.display_name}'s review`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDismissing(false)}>
              Cancel
            </Button>
            <Button variant="danger" disabled={!message.trim()} loading={dismiss.isPending} onClick={() => dismiss.mutate()}>
              Dismiss review
            </Button>
          </>
        }
      >
        <p className="text-fg-muted">The review stops counting towards merge requirements. Say why.</p>
        <Input value={message} onChange={(e) => setMessage(e.target.value)} aria-label="Reason" autoFocus />
        {dismiss.isError && <Alert>{errorMessage(dismiss.error)}</Alert>}
      </Dialog>
    </div>
  )
}

function CommentComposer({ repo, pr, canEdit }: { repo: string; pr: PullRequest; canEdit: boolean }) {
  const invalidate = useInvalidatePull(repo, pr.number)
  const toast = useToast()
  const me = useQuery(sessionQuery).data!.user
  const [body, setBody] = useState('')
  const send = useMutation({
    mutationFn: () => unwrap(api.POST('/repos/{repo}/pulls/{number}/comments', { params: { path: { repo, number: pr.number } }, body: { body, side: 'new' } })),
    onSuccess: () => {
      setBody('')
      invalidate()
    },
  })
  const setState = useMutation({
    mutationFn: (state: 'open' | 'closed') =>
      unwrap(api.PATCH('/repos/{repo}/pulls/{number}', { params: { path: { repo, number: pr.number } }, body: { state } })),
    onSuccess: (_, state) => {
      invalidate()
      if (state === 'closed') toast.info(`Closed #${pr.number}`, { action: pr.head.exists ? { label: 'Reopen', onClick: () => setState.mutate('open') } : undefined })
    },
  })
  return (
    <div className="relative mt-2 flex gap-3 border-t border-border pt-5">
      <Avatar user={me} size={28} className="mt-1 hidden ring-4 ring-bg sm:inline-flex" />
      <div className="flex min-w-0 flex-1 flex-col gap-2">
      <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Comment" placeholder="Leave a comment" onSubmit={() => body.trim() && send.mutate()} />
      {(send.isError || setState.isError) && <Alert>{errorMessage(send.error ?? setState.error)}</Alert>}
      <div className="flex justify-end gap-2">
        {canEdit && pr.state === 'open' && (
          <Button onClick={() => setState.mutate('closed')} loading={setState.isPending} icon={<GitPullRequestClosed size={13} className="text-danger" />}>
            Close pull request
          </Button>
        )}
        {canEdit && pr.state === 'closed' && (
          <Button onClick={() => setState.mutate('open')} loading={setState.isPending} disabled={!pr.head.exists} title={pr.head.exists ? undefined : 'The head branch was deleted'}>
            Reopen pull request
          </Button>
        )}
        <Button variant="primary" disabled={!body.trim()} loading={send.isPending} onClick={() => send.mutate()}>
          Comment
        </Button>
      </div>
      </div>
    </div>
  )
}

const SidebarSection = MetaSection

function PullSidebar({ repo, pr, viewer }: { repo: string; pr: PullRequest; viewer: ReturnType<typeof useViewer> }) {
  const invalidate = useInvalidatePull(repo, pr.number)
  const patch = useMutation({
    mutationFn: (body: { reviewers?: string[]; draft?: boolean } & MetaPatch) =>
      unwrap(api.PATCH('/repos/{repo}/pulls/{number}', { params: { path: { repo, number: pr.number } }, body })),
    onSuccess: invalidate,
  })
  const requested = pr.requested_reviewers.map((u) => u.username)
  const rule = pr.protection
  const stateLabel: Record<string, string> = {
    approved: 'Approved',
    changes_requested: 'Changes requested',
    commented: 'Commented',
    dismissed: 'Review dismissed',
    requested: 'Review pending',
  }
  return (
    <aside className="flex flex-col gap-4 text-base md:sticky md:top-5 md:self-start">
      <SidebarSection
        title="Reviewers"
        action={pr.state === 'open' && viewer.canTriage && <ReviewerPicker value={requested} exclude={[pr.author.username]} onChange={(r) => patch.mutate({ reviewers: r })} />}
      >
        {pr.reviewers.length === 0 ? (
          <p className="text-sm text-fg-faint">No reviews yet.</p>
        ) : (
          <ul className="flex flex-col gap-2" aria-label="Reviewers">
            {pr.reviewers.map((r) => (
              <li key={r.user.id} className="flex items-center gap-2">
                <Avatar user={r.user} size={20} />
                <span className="min-w-0 flex-1 truncate">{r.user.display_name}</span>
                {r.stale && r.state !== 'requested' && <span className="text-2xs text-fg-faint">older commit</span>}
                <Tooltip content={stateLabel[r.state] ?? r.state} describe={false}>
                  <span tabIndex={0} role="img" aria-label={stateLabel[r.state] ?? r.state} className="inline-flex rounded focus-visible:outline-2 focus-visible:outline-accent">
                    <ReviewStateIcon state={r.state} />
                  </span>
                </Tooltip>
              </li>
            ))}
          </ul>
        )}
        {patch.isError && <Alert>{errorMessage(patch.error)}</Alert>}
      </SidebarSection>
      <MetaSections
        repo={repo}
        labels={pr.labels}
        assignees={pr.assignees}
        milestone={pr.milestone}
        canEdit={viewer.canTriage}
        onPatch={(p) => patch.mutate(p)}
        me={viewer.me.username}
      />
      {rule && (
        <SidebarSection title="Branch protection">
          <ul className="flex flex-col gap-1 text-sm text-fg-muted">
            <li className="flex items-center gap-1.5">
              <Shield size={12} /> <span className="font-mono">{rule.pattern}</span>
            </li>
            {rule.required_approvals > 0 && (
              <li>
                {rule.required_approvals} approval{rule.required_approvals === 1 ? '' : 's'} required
                {rule.dismiss_stale_approvals && ', re-approve after new pushes'}
              </li>
            )}
            {rule.require_up_to_date && <li>Must be up to date with {pr.base.branch}</li>}
            {rule.admins_bypass && <li>Admins may bypass</li>}
          </ul>
        </SidebarSection>
      )}
      {pr.state === 'open' && viewer.canEdit && (
        <SidebarSection title="Status">
          {pr.draft ? (
            <div className="flex flex-col gap-2">
              <p className="text-sm text-fg-muted">Draft — reviewers aren't asked yet.</p>
              <Button className="w-full" onClick={() => patch.mutate({ draft: false })} loading={patch.isPending} icon={<Check size={13} />}>
                Ready for review
              </Button>
            </div>
          ) : (
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm text-fg">Ready for review</span>
              <Button size="sm" onClick={() => patch.mutate({ draft: true })} loading={patch.isPending}>
                Convert to draft
              </Button>
            </div>
          )}
        </SidebarSection>
      )}
      <SidebarSection title="Changes">
        <p className="flex items-center gap-2 text-sm text-fg-muted">
          {pr.changed_files} file{pr.changed_files === 1 ? '' : 's'} <DiffStat additions={pr.additions} deletions={pr.deletions} bar />
        </p>
      </SidebarSection>
    </aside>
  )
}

// ---- commits ----

function CommitsTab({ repo, pr }: { repo: string; pr: PullRequest }) {
  const commits = useQuery(pullCommitsQuery(repo, pr.number))
  return (
    <div className="mx-auto w-full max-w-7xl px-6 py-6">
      {commits.isPending ? <SkeletonRows rows={4} avatar /> : commits.isError ? <Alert>{errorMessage(commits.error)}</Alert> : <CommitList repo={repo} commits={commits.data} />}
    </div>
  )
}

// ---- files ----

function FilesTab({ repo, pr, pending, setPending }: { repo: string; pr: PullRequest; pending: Pending[]; setPending: (p: Pending[]) => void }) {
  const files = useQuery(pullFilesQuery(repo, pr.number))
  const comments = useQuery(pullCommentsQuery(repo, pr.number))
  const v = useViewer(pr)
  const invalidate = useInvalidatePull(repo, pr.number)
  const [composer, setComposer] = useState<{ key: string; path: string; side: 'old' | 'new'; line: number } | null>(null)
  const [draft, setDraft] = useState('')
  const anchored = useMemo(() => anchorThreads(groupThreads(comments.data ?? [])), [comments.data])
  const outdatedCount = useMemo(() => groupThreads(comments.data ?? []).filter((t) => t.root.outdated).length, [comments.data])
  const single = useMutation({
    mutationFn: (c: { path: string; side: 'old' | 'new'; line: number; body: string }) =>
      unwrap(api.POST('/repos/{repo}/pulls/{number}/comments', { params: { path: { repo, number: pr.number } }, body: c })),
    onSuccess: () => {
      setComposer(null)
      setDraft('')
      invalidate()
    },
  })
  const canComment = pr.state !== 'closed' && v.canTriage

  const annotations: DiffAnnotations = {
    onComment: canComment
      ? (path, side, line) => {
          setComposer({ key: lineKey(path, side, line), path, side, line })
          setDraft('')
          single.reset()
        }
      : undefined,
    render: (path, side, line) => {
      const key = lineKey(path, side, line)
      const threads = anchored.get(key) ?? []
      const mine = pending.filter((p) => p.key === key)
      const open = composer?.key === key
      if (!threads.length && !mine.length && !open) return null
      return (
        <>
          {threads.map((t) => (
            <ThreadView key={t.root.id} repo={repo} number={pr.number} thread={t} canTriage={v.canTriage} isRepoAdmin={v.isRepoAdmin} />
          ))}
          {mine.map((p) => (
            <div key={p.body + p.line} className="rounded-lg border border-warning/40 bg-surface-1 px-3 py-2 shadow-card">
              <div className="mb-1 flex items-center gap-2 text-sm text-fg-muted">
                <Badge tone="warning">pending</Badge> Part of your review
                <span className="flex-1" />
                <Button variant="ghost" size="xs" className="hover:text-danger" onClick={() => setPending(pending.filter((x) => x !== p))}>
                  Remove
                </Button>
              </div>
              <p className="whitespace-pre-wrap">{p.body}</p>
            </div>
          ))}
          {open && composer && (
            <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface-1 p-2 shadow-card">
              <MarkdownEditor value={draft} onChange={setDraft} repo={repo} label="Line comment" placeholder="Comment on this line" autoFocus minRows={3} />
              {single.isError && <Alert>{errorMessage(single.error)}</Alert>}
              <div className="flex justify-end gap-2">
                <Button variant="ghost" onClick={() => setComposer(null)}>
                  Cancel
                </Button>
                <Button disabled={!draft.trim()} loading={single.isPending} onClick={() => single.mutate({ path: composer.path, side: composer.side, line: composer.line, body: draft })}>
                  Add single comment
                </Button>
                <Button
                  variant="primary"
                  disabled={!draft.trim()}
                  onClick={() => {
                    setPending([...pending, { ...composer, body: draft }])
                    setComposer(null)
                    setDraft('')
                  }}
                >
                  {pending.length ? 'Add to review' : 'Start a review'}
                </Button>
              </div>
            </div>
          )}
        </>
      )
    },
  }

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-3 px-6 py-6">
      {outdatedCount > 0 && (
        <Alert tone="info">
          {outdatedCount} comment thread{outdatedCount === 1 ? ' is' : 's are'} on code that changed since; see the Conversation tab.
        </Alert>
      )}
      {files.isPending ? <SkeletonRows rows={4} /> : files.isError ? <Alert>{errorMessage(files.error)}</Alert> : <DiffView diff={files.data} annotations={annotations} />}
    </div>
  )
}

function ReviewDialog({
  repo,
  pr,
  open,
  onClose,
  pending,
  onSubmitted,
}: {
  repo: string
  pr: PullRequest
  open: boolean
  onClose: () => void
  pending: Pending[]
  onSubmitted: () => void
}) {
  const v = useViewer(pr)
  const invalidate = useInvalidatePull(repo, pr.number)
  const navigate = useNavigate()
  const [state, setState] = useState<'comment' | 'approve' | 'request_changes'>('comment')
  const [body, setBody] = useState('')
  const submit = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/repos/{repo}/pulls/{number}/reviews', {
          params: { path: { repo, number: pr.number } },
          body: { state, body, commit_sha: pr.head.sha, comments: pending.map(({ path, side, line, body }) => ({ path, side, line, body })) },
        }),
      ),
    onSuccess: () => {
      setBody('')
      setState('comment')
      onSubmitted()
      onClose()
      invalidate()
      void navigate({ to: '/$repo/pulls/$number', params: { repo, number: String(pr.number) }, search: {} })
    },
  })
  const options: { value: typeof state; label: string; hint: string; icon: ReactNode }[] = [
    { value: 'comment', label: 'Comment', hint: 'General feedback without a verdict.', icon: <MessageSquare size={14} /> },
    ...(v.isAuthor
      ? []
      : ([
          { value: 'approve', label: 'Approve', hint: 'Ready to merge.', icon: <CheckCircle2 size={14} className="text-success" /> },
          { value: 'request_changes', label: 'Request changes', hint: 'Must be addressed before merging.', icon: <XCircle size={14} className="text-danger" /> },
        ] as const)),
  ]
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Submit review"
      width="max-w-lg"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" loading={submit.isPending} onClick={() => submit.mutate()}>
            Submit review
          </Button>
        </>
      }
    >
      <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Review summary" placeholder="Leave a summary (optional for approvals)" minRows={4} />
      <fieldset className="flex flex-col gap-1.5">
        <legend className="sr-only">Verdict</legend>
        {options.map((o) => (
          <RadioCard key={o.value} name="verdict" value={o.value} checked={state === o.value} onChange={() => setState(o.value)} icon={o.icon} title={o.label} description={o.hint} />
        ))}
      </fieldset>
      {v.isAuthor && <p className="text-sm text-fg-faint">You can't approve or request changes on your own pull request.</p>}
      {pending.length > 0 && (
        <p className="text-sm text-fg-muted">
          {pending.length} pending inline comment{pending.length === 1 ? '' : 's'} will be submitted with this review.
        </p>
      )}
      {submit.isError && <Alert>{errorMessage(submit.error)}</Alert>}
    </Dialog>
  )
}
