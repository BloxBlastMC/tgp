import { useMutation, useQuery } from '@tanstack/react-query'
import { Navigate, useParams } from '@tanstack/react-router'
import { CheckCircle2, ChevronDown, CircleSlash, Pencil, RefreshCw } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { api, ApiRequestError, errorMessage, unwrap, type Issue, type Schemas } from '../../api/client'
import { issueQuery, issueTimelineQuery, sessionQuery } from '../../api/queries'
import { EventLine } from '../../components/issues/EventLine'
import { IssueStateBadge, MetaSections, type MetaPatch } from '../../components/issues/meta'
import { EmptyState } from '../../components/Page'
import { Avatar, CommentCard, MarkdownEditor, useInvalidatePull } from '../../components/pulls/common'
import { Alert, Button, Input, Spinner, Time } from '../../components/ui'
import { useRepoName } from '../repos/RepoLayout'

export function IssuePage() {
  const repo = useRepoName()
  const { number: numStr } = useParams({ strict: false }) as { number: string }
  const number = Number(numStr)
  const valid = Number.isInteger(number) && number > 0
  const issue = useQuery({ ...issueQuery(repo, number), enabled: valid })
  if (!valid) return <EmptyState title="Issue not found" />
  if (issue.isPending) return <Spinner />
  if (issue.isError) {
    const e = issue.error
    if (e instanceof ApiRequestError && e.status === 404 && e.details?.is_pull)
      return <Navigate to="/$repo/pulls/$number" params={{ repo, number: String(number) }} replace />
    if (e instanceof ApiRequestError && e.status === 404) return <EmptyState title="Issue not found">#{number} doesn't exist in {repo}.</EmptyState>
    return (
      <div className="p-4">
        <Alert>{errorMessage(e)}</Alert>
      </div>
    )
  }
  return <IssueView repo={repo} issue={issue.data} />
}

function useIssueViewer(issue: Issue) {
  const me = useQuery(sessionQuery).data!.user
  const perm = issue.viewer_permission
  const canTriage = perm === 'triage' || perm === 'write' || perm === 'admin'
  return { me, canTriage, isRepoAdmin: perm === 'admin', canEdit: issue.can_edit }
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
      <div className="mx-auto grid w-full max-w-6xl gap-6 p-4 md:grid-cols-[minmax(0,1fr)_240px]">
        <div className="flex min-w-0 flex-col gap-3">
          <BodyCard repo={repo} issue={issue} canEdit={v.canEdit} />
          {timeline.isPending ? (
            <Spinner />
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
        <aside className="flex flex-col gap-3 text-[13px]">
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
    <div className="border-b border-border px-4 py-3">
      {editing ? (
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            patch.mutate({ title }, { onSuccess: () => setEditing(false) })
          }}
        >
          <Input value={title} onChange={(e) => setTitle(e.target.value)} aria-label="Title" autoFocus className="text-[15px]" />
          <Button type="submit" variant="primary" loading={patch.isPending} disabled={!title.trim()}>
            Save
          </Button>
          <Button variant="ghost" onClick={() => setEditing(false)}>
            Cancel
          </Button>
        </form>
      ) : (
        <div className="flex items-start gap-2">
          <h2 className="min-w-0 flex-1 text-[17px] font-semibold">
            {issue.title} <span className="font-normal text-fg-faint">#{issue.number}</span>
          </h2>
          {canEdit && (
            <Button
              variant="ghost"
              onClick={() => {
                setTitle(issue.title)
                setEditing(true)
              }}
            >
              <Pencil size={13} /> Edit
            </Button>
          )}
        </div>
      )}
      {patch.isError && <Alert>{errorMessage(patch.error)}</Alert>}
      <div className="mt-1.5 flex flex-wrap items-center gap-2 text-fg-muted">
        <IssueStateBadge issue={issue} />
        <span>
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
    <div className="rounded-md border border-border">
      <div className="flex items-center gap-2 border-b border-border bg-bg-subtle px-3 py-1.5 text-[12px] text-fg-muted">
        <Avatar user={issue.author} size={18} />
        <span className="font-medium text-fg">{issue.author.display_name}</span> opened this <Time iso={issue.created_at} />
        <span className="flex-1" />
        {canEdit && !editing && (
          <button
            type="button"
            className="rounded p-0.5 hover:bg-bg-hover hover:text-fg"
            aria-label="Edit description"
            onClick={() => {
              setBody(issue.body)
              setEditing(true)
            }}
          >
            <Pencil size={12} />
          </button>
        )}
      </div>
      <div className="px-3 py-2">
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
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])
  const suffix = withComment ? ' with comment' : ''
  return (
    <div className="relative flex" ref={ref}>
      <Button className="rounded-r-none" loading={loading} onClick={() => onClose('completed')}>
        <CheckCircle2 size={13} className="text-[#a371f7]" /> Close issue{suffix}
      </Button>
      <Button className="rounded-l-none border-l-0 px-1.5" onClick={() => setOpen(!open)} aria-label="More ways to close" aria-expanded={open}>
        <ChevronDown size={13} />
      </Button>
      {open && (
        <ul className="absolute right-0 bottom-8 z-20 w-64 rounded-md border border-border-strong bg-bg-elevated p-1 shadow-xl" role="menu">
          <li>
            <button
              type="button"
              role="menuitem"
              className="flex w-full items-start gap-2 rounded px-2 py-1.5 text-left hover:bg-bg-hover"
              onClick={() => {
                setOpen(false)
                onClose('completed')
              }}
            >
              <CheckCircle2 size={14} className="mt-0.5 text-[#a371f7]" />
              <span>
                Close as completed
                <span className="block text-[11px] text-fg-faint">Done, fixed or resolved</span>
              </span>
            </button>
          </li>
          <li>
            <button
              type="button"
              role="menuitem"
              className="flex w-full items-start gap-2 rounded px-2 py-1.5 text-left hover:bg-bg-hover"
              onClick={() => {
                setOpen(false)
                onClose('not_planned')
              }}
            >
              <CircleSlash size={14} className="mt-0.5 text-fg-faint" />
              <span>
                Close as not planned
                <span className="block text-[11px] text-fg-faint">Won't fix, duplicate, can't reproduce</span>
              </span>
            </button>
          </li>
        </ul>
      )}
    </div>
  )
}

function Composer({ repo, issue }: { repo: string; issue: Issue }) {
  const invalidate = useInvalidatePull(repo, issue.number)
  const patch = usePatchIssue(repo, issue.number)
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
    patch.mutate({ state, state_reason: reason })
  }
  return (
    <div className="mt-2 flex flex-col gap-2 border-t border-border pt-4">
      <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Comment" placeholder="Leave a comment" onSubmit={() => body.trim() && send.mutate(body)} />
      {(send.isError || patch.isError) && <Alert>{errorMessage(send.error ?? patch.error)}</Alert>}
      <div className="flex justify-end gap-2">
        {issue.state === 'open' ? (
          <CloseButton loading={patch.isPending} withComment={!!body.trim()} onClose={(reason) => void withComment('closed', reason)} />
        ) : (
          <Button loading={patch.isPending} onClick={() => void withComment('open')}>
            <RefreshCw size={13} /> Reopen issue{body.trim() ? ' with comment' : ''}
          </Button>
        )}
        <Button variant="primary" disabled={!body.trim()} loading={send.isPending} onClick={() => send.mutate(body)}>
          Comment
        </Button>
      </div>
    </div>
  )
}
