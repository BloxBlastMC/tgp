import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, CircleDot, GitMerge, GitPullRequest, GitPullRequestClosed, GitPullRequestDraft, MoreHorizontal, Pencil, SquarePen, Trash2, XCircle } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { api, errorMessage, unwrap, type Comment, type PullRequest, type PullSummary } from '../../api/client'
import { pullKeyPrefix, sessionQuery } from '../../api/queries'
import { modKey } from '../../lib/commands'
import { decisionInfo, type Thread } from '../../lib/pulls'
import { pullState } from '../../lib/states'
import { Alert, Avatar, Badge, Button, cx, IconButton, Menu, MenuItem, Pill, Textarea, Time, useConfirm } from '../ui'

export { Avatar }

type StateLike = Pick<PullSummary, 'state' | 'draft'>

export function PullStateIcon({ pr, size = 15 }: { pr: StateLike; size?: number }) {
  if (pr.state === 'merged') return <GitMerge size={size} className="shrink-0 text-done" aria-label="merged" />
  if (pr.state === 'closed') return <GitPullRequestClosed size={size} className="shrink-0 text-danger" aria-label="closed" />
  if (pr.draft) return <GitPullRequestDraft size={size} className="shrink-0 text-fg-faint" aria-label="draft" />
  return <GitPullRequest size={size} className="shrink-0 text-success" aria-label="open" />
}

export function PullStateBadge({ pr }: { pr: StateLike }) {
  const st = pullState(pr)
  const Icon = st.key === 'merged' ? GitMerge : st.key === 'closed' ? GitPullRequestClosed : st.key === 'draft' ? GitPullRequestDraft : GitPullRequest
  return (
    <Pill tone={st.tone} icon={<Icon size={13} />}>
      {st.label}
    </Pill>
  )
}

export function DecisionBadge({ decision }: { decision: PullRequest['review_decision'] }) {
  const d = decisionInfo(decision)
  if (!d) return null
  return <Badge tone={d.tone}>{d.label}</Badge>
}

export function BranchName({ children }: { children: ReactNode }) {
  return <code className="rounded-md bg-accent-subtle px-1.5 py-0.5 font-mono text-sm text-accent">{children}</code>
}

/** Textarea with Write / Preview tabs; preview is rendered by the server. */
export function MarkdownEditor({
  value,
  onChange,
  repo,
  placeholder,
  label,
  autoFocus,
  minRows = 4,
  onSubmit,
}: {
  value: string
  onChange: (v: string) => void
  repo: string
  placeholder?: string
  label: string
  autoFocus?: boolean
  minRows?: number
  onSubmit?: () => void
}) {
  const [tab, setTab] = useState<'write' | 'preview'>('write')
  const preview = useQuery({
    queryKey: ['markdown', repo, value],
    queryFn: () => unwrap(api.POST('/markdown', { body: { text: value, repo } })),
    enabled: tab === 'preview' && value.trim() !== '',
    staleTime: Infinity,
  })
  return (
    <div className="overflow-hidden rounded-lg border border-border-strong bg-bg transition-[border-color,box-shadow] focus-within:border-accent focus-within:shadow-[0_0_0_3px_var(--accent-subtle)]">
      <div className="flex items-center gap-0.5 border-b border-border bg-bg-subtle px-1.5 pt-1.5 text-sm">
        {(['write', 'preview'] as const).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            className={cx(
              '-mb-px rounded-t-md border border-b-0 px-3 py-1 font-medium capitalize',
              tab === t ? 'border-border bg-bg text-fg' : 'border-transparent text-fg-muted hover:text-fg',
            )}
            aria-pressed={tab === t}
          >
            {t}
          </button>
        ))}
        <span className="flex-1" />
        <span className="flex items-center gap-1 self-center px-1.5 pb-1 text-xs text-fg-faint">
          <SquarePen size={11} /> Markdown supported{onSubmit && ` · ${modKey()}+Enter to submit`}
        </span>
      </div>
      {tab === 'write' ? (
        <Textarea
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          aria-label={label}
          autoFocus={autoFocus}
          rows={minRows}
          onKeyDown={(e) => {
            if (onSubmit && (e.metaKey || e.ctrlKey) && e.key === 'Enter') {
              e.preventDefault()
              onSubmit()
            }
          }}
          className="rounded-none border-0 bg-transparent focus:border-0 focus:shadow-none"
        />
      ) : (
        <div className="min-h-20 px-3 py-2.5">
          {value.trim() === '' ? (
            <p className="text-fg-faint">Nothing to preview.</p>
          ) : preview.data ? (
            <div className="markdown" dangerouslySetInnerHTML={{ __html: preview.data.html }} />
          ) : (
            <p className="text-fg-faint">Rendering…</p>
          )}
        </div>
      )}
    </div>
  )
}

/** Invalidates every query about one pull request or issue (they share
 * numbers), plus PR/issue lists and label/milestone counts. */
export function useInvalidatePull(repo: string, number: number) {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: pullKeyPrefix(repo, number) })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'pulls'] })
    void qc.invalidateQueries({ queryKey: ['pulls'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'branches'] })
    // Issues share numbers and comment endpoints with pull requests, and
    // label/milestone counts include both.
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'issue', number] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'issues'] })
    void qc.invalidateQueries({ queryKey: ['issues'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'labels'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'milestones'] })
  }
}

function CommentMenu({ items }: { items: { label: string; onClick: () => void; danger?: boolean; icon?: ReactNode }[] }) {
  if (items.length === 0) return null
  return (
    <Menu
      placement="bottom-end"
      label="Comment actions"
      className="min-w-36"
      trigger={(t) => (
        <IconButton {...t} label="Comment actions" size="xs" tooltip={false}>
          <MoreHorizontal size={14} />
        </IconButton>
      )}
    >
      {items.map((it) => (
        <MenuItem key={it.label} onSelect={it.onClick} tone={it.danger ? 'danger' : undefined} icon={it.icon}>
          {it.label}
        </MenuItem>
      ))}
    </Menu>
  )
}

/** A single comment with edit/delete for its author (delete also for admins). */
export function CommentCard({ repo, number, comment, isRepoAdmin, compact }: { repo: string; number: number; comment: Comment; isRepoAdmin: boolean; compact?: boolean }) {
  const me = useQuery(sessionQuery).data?.user
  const invalidate = useInvalidatePull(repo, number)
  const confirm = useConfirm()
  const [editing, setEditing] = useState(false)
  const [body, setBody] = useState(comment.body)
  const save = useMutation({
    mutationFn: () => unwrap(api.PATCH('/repos/{repo}/comments/{id}', { params: { path: { repo, id: comment.id } }, body: { body } })),
    onSuccess: () => {
      setEditing(false)
      invalidate()
    },
  })
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/repos/{repo}/comments/{id}', { params: { path: { repo, id: comment.id } } })),
    onSuccess: invalidate,
  })
  const mine = me?.id === comment.author.id
  const items = [
    ...(mine ? [{ label: 'Edit', icon: <Pencil size={13} />, onClick: () => (setBody(comment.body), setEditing(true)) }] : []),
    ...(mine || isRepoAdmin
      ? [
          {
            label: 'Delete',
            danger: true,
            icon: <Trash2 size={13} />,
            onClick: async () => {
              if (await confirm({ title: 'Delete this comment?', description: "It's removed for everyone. This can't be undone.", confirmLabel: 'Delete comment' })) del.mutate()
            },
          },
        ]
      : []),
  ]
  return (
    <div className={cx('flex gap-2.5', compact ? '' : 'overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card')} data-testid="comment">
      {compact && <Avatar user={comment.author} size={22} />}
      <div className="min-w-0 flex-1">
        <div className={cx('flex items-center gap-2 text-sm text-fg-muted', !compact && 'border-b border-border bg-bg-subtle px-3 py-1.5')}>
          {!compact && <Avatar user={comment.author} size={18} />}
          <span className="font-medium text-fg">{comment.author.display_name}</span>
          <span className="text-fg-faint">
            <Time iso={comment.created_at} />
            {comment.edited && ' · edited'}
          </span>
          <span className="flex-1" />
          <CommentMenu items={items} />
        </div>
        <div className={cx(compact ? 'mt-0.5' : 'px-4 py-3')}>
          {editing ? (
            <div className="flex flex-col gap-2">
              <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Edit comment" autoFocus onSubmit={() => save.mutate()} />
              {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
              <div className="flex justify-end gap-2">
                <Button variant="ghost" onClick={() => setEditing(false)}>
                  Cancel
                </Button>
                <Button variant="primary" loading={save.isPending} disabled={!body.trim()} onClick={() => save.mutate()}>
                  Save
                </Button>
              </div>
            </div>
          ) : (
            <div className="markdown" dangerouslySetInnerHTML={{ __html: comment.body_html }} />
          )}
          {del.isError && <Alert>{errorMessage(del.error)}</Alert>}
        </div>
      </div>
    </div>
  )
}

export function DiffHunk({ hunk }: { hunk: string }) {
  const lines = hunk.split('\n')
  return (
    <pre className="overflow-x-auto border-b border-border bg-bg font-mono text-sm">
      {lines.map((l, i) => (
        <div
          key={i}
          className={cx(
            'px-3 whitespace-pre',
            i === 0 ? 'bg-accent-subtle text-fg-muted' : l.startsWith('+') ? 'bg-success/10' : l.startsWith('-') ? 'bg-danger/10' : '',
            i === lines.length - 1 && i > 0 && 'font-semibold',
          )}
        >
          {l}
        </div>
      ))}
    </pre>
  )
}

/** An inline comment thread with replies, resolve and reply box. */
export function ThreadView({
  repo,
  number,
  thread,
  canTriage,
  isRepoAdmin,
  showContext,
}: {
  repo: string
  number: number
  thread: Thread
  canTriage: boolean
  isRepoAdmin: boolean
  showContext?: boolean
}) {
  const { root, replies } = thread
  const invalidate = useInvalidatePull(repo, number)
  const [collapsed, setCollapsed] = useState(!!root.resolved)
  const [replying, setReplying] = useState(false)
  const [reply, setReply] = useState('')
  const send = useMutation({
    mutationFn: () => unwrap(api.POST('/repos/{repo}/pulls/{number}/comments', { params: { path: { repo, number } }, body: { body: reply, reply_to: root.id, side: root.side ?? 'new' } })),
    onSuccess: () => {
      setReply('')
      setReplying(false)
      invalidate()
    },
  })
  const resolve = useMutation({
    mutationFn: (resolved: boolean) => unwrap(api.PATCH('/repos/{repo}/comments/{id}', { params: { path: { repo, id: root.id } }, body: { resolved } })),
    onSuccess: (_, resolved) => {
      setCollapsed(resolved)
      invalidate()
    },
  })
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card" data-testid="thread">
      {showContext && (
        <div className="flex items-center gap-2 border-b border-border bg-bg-subtle px-3 py-1.5 text-sm">
          <span className="min-w-0 truncate font-mono">{root.path}</span>
          <span className="text-fg-faint">
            line {root.line}
            {root.side === 'old' && ' (old)'}
          </span>
          {root.outdated && <Badge tone="warning">outdated</Badge>}
          {root.resolved && <Badge tone="success">resolved</Badge>}
          <span className="flex-1" />
          {root.resolved && (
            <button type="button" className="text-fg-muted hover:text-fg" onClick={() => setCollapsed(!collapsed)}>
              {collapsed ? 'Show' : 'Hide'}
            </button>
          )}
        </div>
      )}
      {showContext && !collapsed && root.diff_hunk && <DiffHunk hunk={root.diff_hunk} />}
      {!showContext && root.resolved && (
        <div className="flex items-center gap-2 border-b border-border bg-bg-subtle px-3 py-1 text-sm text-fg-muted">
          <CheckCircle2 size={12} className="text-success" /> Resolved by {root.resolved_by?.display_name ?? 'someone'}
          <span className="flex-1" />
          <button type="button" className="hover:text-fg" onClick={() => setCollapsed(!collapsed)}>
            {collapsed ? 'Show' : 'Hide'}
          </button>
        </div>
      )}
      {!collapsed && (
        <>
          <div className="flex flex-col gap-3 px-3 py-2.5">
            {[root, ...replies].map((c) => (
              <CommentCard key={c.id} repo={repo} number={number} comment={c} isRepoAdmin={isRepoAdmin} compact />
            ))}
          </div>
          {canTriage && (
            <div className="border-t border-border px-3 py-2">
              {replying ? (
                <div className="flex flex-col gap-2">
                  <MarkdownEditor value={reply} onChange={setReply} repo={repo} label="Reply" placeholder="Reply…" autoFocus minRows={2} onSubmit={() => reply.trim() && send.mutate()} />
                  {send.isError && <Alert>{errorMessage(send.error)}</Alert>}
                  <div className="flex justify-end gap-2">
                    <Button variant="ghost" onClick={() => setReplying(false)}>
                      Cancel
                    </Button>
                    <Button variant="primary" loading={send.isPending} disabled={!reply.trim()} onClick={() => send.mutate()}>
                      Reply
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="flex items-center gap-2">
                  <button type="button" onClick={() => setReplying(true)} className="h-7 flex-1 rounded-md border border-border bg-bg px-2.5 text-left text-fg-faint transition-colors hover:border-border-strong">
                    Reply…
                  </button>
                  <Button variant="ghost" loading={resolve.isPending} onClick={() => resolve.mutate(!root.resolved)}>
                    {root.resolved ? 'Unresolve' : 'Resolve'}
                  </Button>
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}

export function ReviewStateIcon({ state }: { state: string }) {
  switch (state) {
    case 'approved':
      return <CheckCircle2 size={14} className="text-success" aria-label="approved" />
    case 'changes_requested':
      return <XCircle size={14} className="text-danger" aria-label="changes requested" />
    case 'requested':
      return <CircleDot size={14} className="text-warning" aria-label="review requested" />
    case 'dismissed':
      return <XCircle size={14} className="text-fg-faint" aria-label="dismissed" />
  }
  return <MoreHorizontal size={14} className="text-fg-muted" aria-label="commented" />
}
