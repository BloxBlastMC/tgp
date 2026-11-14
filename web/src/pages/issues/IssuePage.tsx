import { useMutation, useQuery } from '@tanstack/react-query'
import { Navigate, useParams } from '@tanstack/react-router'
import { CheckCircle2, CircleDot, CircleSlash, Pencil, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { api, ApiRequestError, errorMessage, unwrap, type Issue, type Schemas } from '../../api/client'
import { issueQuery, issueTimelineQuery, sessionQuery } from '../../api/queries'
import { EventLine } from '../../components/issues/EventLine'
import { IssueStateBadge, MetaSections, type MetaPatch } from '../../components/issues/meta'
import { EmptyState } from '../../components/Page'
import { Avatar, CommentCard, MarkdownEditor, useInvalidatePull } from '../../components/pulls/common'
import { Alert, Badge, Button, ErrorState, IconButton, Input, MenuItem, PageSkeleton, SkeletonRows, SplitButton, Time, useToast } from '../../components/ui'
import { canTriage as canTriagePerm, isRepoAdmin } from '../../lib/permissions'
import { useRepoName } from '../repos/RepoLayout'

export function IssuePage() {
  const repo = useRepoName()
  const { number: numStr } = useParams({ strict: false }) as { number: string }
  const number = Number(numStr)
  const valid = Number.isInteger(number) && number > 0
  const issue = useQuery({ ...issueQuery(repo, number), enabled: valid })
  if (!valid) return <EmptyState icon={<CircleDot size={20} />} title="Issue not found" />
  if (issue.isPending) return <PageSkeleton rows={3} />
  if (issue.isError) {
    const e = issue.error
    if (e instanceof ApiRequestError && e.status === 404 && e.details?.is_pull)
      return <Navigate to="/$repo/pulls/$number" params={{ repo, number: String(number) }} replace />
    if (e instanceof ApiRequestError && e.status === 404)
      return (
        <EmptyState icon={<CircleDot size={20} />} title="Issue not found">
          #{number} doesn't exist in {repo}.
        </EmptyState>
      )
    return <ErrorState error={e} />
  }
  return <IssueView repo={repo} issue={issue.data} />
}

function useIssueViewer(issue: Issue) {
  const me = useQuery(sessionQuery).data!.user
  const perm = issue.viewer_permission
  return { me, canTriage: canTriagePerm(perm), isRepoAdmin: isRepoAdmin(perm), canEdit: issue.can_edit }
}

function usePatchIssue(repo: string, number: number) {
  const invalidate = useInvalidatePull(repo, number)
  return useMutation({
    mutationFn: (body: Schemas['UpdateIssue']) =>
      unwrap(api.PATCH('/repos/{repo}/issues/{number}', { params: { path: { repo, number } }, body })),
    onSuccess: invalidate,
  })
}

function IssueView({ repo, issue }: { repo: string; issue: Issue }) {
  const v = useIssueViewer(issue)
  const timeline = useQuery(issueTimelineQuery(repo, issue.number))
  const patch = usePatchIssue(repo, issue.number)
  const onMeta = (p: MetaPatch) => patch.mutate(p)
  return (
    <div className="flex flex-col">
      <IssueHeader repo={repo} issue={issue} canEdit={v.canEdit} />
      <div className="mx-auto grid w-full max-w-6xl gap-8 px-6 py-5 md:grid-cols-[minmax(0,1fr)_256px]">
        <div className="relative flex min-w-0 flex-col gap-4 before:absolute before:top-4 before:bottom-24 before:left-[19px] before:w-px before:bg-border [&>*]:relative">
          <BodyCard repo={repo} issue={issue} canEdit={v.canEdit} />
          {timeline.isPending ? (
            <SkeletonRows rows={3} />
          ) : timeline.isError ? (
            <Alert>{errorMessage(timeline.error)}</Alert>
          ) : (
            timeline.data.map((it, i) => {
              if (it.kind === 'event' && it.event) return <EventLine key={`e${it.event.id}`} repo={repo} event={it.event} isIssue />
              if (it.comment) return <CommentCard key={`c${it.comment.id}`} repo={repo} number={issue.number} comment={it.comment} isRepoAdmin={v.isRepoAdmin} />
              return <span key={i} />
            })
          )}
          {v.canTriage && <Composer repo={repo} issue={issue} />}
        </div>
        <aside className="flex flex-col gap-4 text-base md:sticky md:top-5 md:self-start">
          <MetaSections
            repo={repo}
            labels={issue.labels}
            assignees={issue.assignees}
            milestone={issue.milestone}
            canEdit={v.canTriage}
            onPatch={onMeta}
            me={v.me.username}
          />
          {patch.isError && <Alert>{errorMessage(patch.error)}</Alert>}
        </aside>
      </div>
    </div>
  )
}

function IssueHeader({ repo, issue, canEdit }: { repo: string; issue: Issue; canEdit: boolean }) {
  const patch = usePatchIssue(repo, issue.number)
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(issue.title)
  return (
    <div className="border-b border-border px-6 py-4">
      {editing ? (
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            patch.mutate({ title }, { onSuccess: () => setEditing(false) })
          }}
        >
          <Input value={title} onChange={(e) => setTitle(e.target.value)} aria-label="Title" autoFocus size="lg" className="text-lg font-semibold" />
          <Button type="submit" variant="primary" loading={patch.isPending} disabled={!title.trim()}>
            Save
          </Button>
          <Button variant="ghost" onClick={() => setEditing(false)}>
            Cancel
          </Button>
        </form>
      ) : (
        <div className="flex items-start gap-2">
          <h1 className="min-w-0 flex-1 text-xl font-semibold tracking-tight">
            {issue.title} <span className="font-normal text-fg-faint">#{issue.number}</span>
          </h1>
          {canEdit && (
            <Button
              variant="ghost"
              icon={<Pencil size={13} />}
              onClick={() => {
                setTitle(issue.title)
                setEditing(true)
              }}
            >
              Edit
            </Button>
          )}
        </div>
      )}
      {patch.isError && <Alert>{errorMessage(patch.error)}</Alert>}
      <div className="mt-2 flex flex-wrap items-center gap-2 text-fg-muted">
        <IssueStateBadge issue={issue} />
        <span className="flex flex-wrap items-center gap-1">
          <Avatar user={issue.author} size={18} />
          <span className="font-medium text-fg">{issue.author.display_name}</span> opened this <Time iso={issue.created_at} /> · {issue.comments} comment
          {issue.comments === 1 ? '' : 's'}
        </span>
      </div>
    </div>
  )
}

function BodyCard({ repo, issue, canEdit }: { repo: string; issue: Issue; canEdit: boolean }) {
  const patch = usePatchIssue(repo, issue.number)
  const [editing, setEditing] = useState(false)
  const [body, setBody] = useState(issue.body)
  return (
    <div className="relative overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
      <div className="flex items-center gap-2 border-b border-border bg-bg-subtle px-3 py-1.5 text-sm text-fg-muted">
        <Avatar user={issue.author} size={18} />
        <span className="font-medium text-fg">{issue.author.display_name}</span> opened this <Time iso={issue.created_at} />
        <Badge>Author</Badge>
        <span className="flex-1" />
        {canEdit && !editing && (
          <IconButton
            label="Edit description"
            size="xs"
            onClick={() => {
              setBody(issue.body)
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
            {patch.isError && <Alert>{errorMessage(patch.error)}</Alert>}
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setEditing(false)}>
                Cancel
              </Button>
              <Button variant="primary" loading={patch.isPending} onClick={() => patch.mutate({ body }, { onSuccess: () => setEditing(false) })}>
                Save
              </Button>
            </div>
          </div>
        ) : issue.body_html ? (
          <div className="markdown" dangerouslySetInnerHTML={{ __html: issue.body_html }} />
        ) : (
          <p className="text-fg-faint italic">No description.</p>
        )}
      </div>
    </div>
  )
}

/** "Close issue" with a menu for the reason. */
function CloseButton({ onClose, loading, withComment }: { onClose: (reason: 'completed' | 'not_planned') => void; loading: boolean; withComment: boolean }) {
  const suffix = withComment ? ' with comment' : ''
  return (
    <SplitButton
      onClick={() => onClose('completed')}
      loading={loading}
      menuLabel="More ways to close"
      icon={<CheckCircle2 size={13} className="text-done" />}
      menu={
        <>
          <MenuItem icon={<CheckCircle2 size={14} className="text-done" />} description="Done, fixed or resolved" onSelect={() => onClose('completed')}>
            Close as completed
          </MenuItem>
          <MenuItem icon={<CircleSlash size={14} />} description="Won't fix, duplicate, can't reproduce" onSelect={() => onClose('not_planned')}>
            Close as not planned
          </MenuItem>
        </>
      }
    >
      Close issue{suffix}
    </SplitButton>
  )
}

function Composer({ repo, issue }: { repo: string; issue: Issue }) {
  const invalidate = useInvalidatePull(repo, issue.number)
  const patch = usePatchIssue(repo, issue.number)
  const toast = useToast()
  const me = useQuery(sessionQuery).data!.user
  const [body, setBody] = useState('')
  const send = useMutation({
    mutationFn: (text: string) => unwrap(api.POST('/repos/{repo}/issues/{number}/comments', { params: { path: { repo, number: issue.number } }, body: { body: text } })),
    onSuccess: () => {
      setBody('')
      invalidate()
    },
  })
  const withComment = async (state: 'open' | 'closed', reason?: 'completed' | 'not_planned') => {
    if (body.trim()) await send.mutateAsync(body)
    patch.mutate(
      { state, state_reason: reason },
      {
        onSuccess: () => {
          if (state === 'closed')
            toast.info(`Closed #${issue.number}${reason === 'not_planned' ? ' (not planned)' : ''}`, { action: { label: 'Reopen', onClick: () => patch.mutate({ state: 'open' }) } })
        },
      },
    )
  }
  return (
    <div className="relative mt-2 flex gap-3 border-t border-border pt-5">
      <Avatar user={me} size={28} className="mt-1 hidden ring-4 ring-bg sm:inline-flex" />
      <div className="flex min-w-0 flex-1 flex-col gap-2">
      <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Comment" placeholder="Leave a comment" onSubmit={() => body.trim() && send.mutate(body)} />
      {(send.isError || patch.isError) && <Alert>{errorMessage(send.error ?? patch.error)}</Alert>}
      <div className="flex justify-end gap-2">
        {issue.state === 'open' ? (
          <CloseButton loading={patch.isPending} withComment={!!body.trim()} onClose={(reason) => void withComment('closed', reason)} />
        ) : (
          <Button loading={patch.isPending} onClick={() => void withComment('open')} icon={<RefreshCw size={13} />}>
            Reopen issue{body.trim() ? ' with comment' : ''}
          </Button>
        )}
        <Button variant="primary" disabled={!body.trim()} loading={send.isPending} onClick={() => send.mutate(body)}>
          Comment
        </Button>
      </div>
      </div>
    </div>
  )
}
