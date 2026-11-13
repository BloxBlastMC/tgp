import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { labelsQuery, milestonesQuery, repoQuery, sessionQuery, usersQuery } from '../../api/queries'
import { MetaSections, type MetaPatch } from '../../components/issues/meta'
import { EmptyState } from '../../components/Page'
import { MarkdownEditor } from '../../components/pulls/common'
import { Alert, Button, Field, Input } from '../../components/ui'
import { useRepoName } from '../repos/RepoLayout'
import { canTriage } from './RepoIssuesPage'

export function NewIssuePage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const me = useQuery(sessionQuery).data!.user
  const navigate = useNavigate()
  const qc = useQueryClient()
  const labels = useQuery(labelsQuery(repo))
  const users = useQuery(usersQuery)
  const milestones = useQuery(milestonesQuery(repo, 'open'))
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [meta, setMeta] = useState<{ labels: string[]; assignees: string[]; milestone: number }>({ labels: [], assignees: [], milestone: 0 })
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/repos/{repo}/issues', {
          params: { path: { repo } },
          body: { title, body, labels: meta.labels, assignees: meta.assignees, milestone: meta.milestone || undefined },
        }),
      ),
    onSuccess: (is) => {
      void qc.invalidateQueries({ queryKey: ['repo', repo, 'issues'] })
      void qc.invalidateQueries({ queryKey: ['issues'] })
      void qc.invalidateQueries({ queryKey: ['repo', repo, 'labels'] })
      void qc.invalidateQueries({ queryKey: ['repo', repo, 'milestones'] })
      void navigate({ to: '/$repo/issues/$number', params: { repo, number: String(is.number) } })
    },
  })
  if (!canTriage(r.permission)) return <EmptyState title="Can't open issues">You need at least triage access to {repo}.</EmptyState>
  const patch = (p: MetaPatch) =>
    setMeta((m) => ({ labels: p.labels ?? m.labels, assignees: p.assignees ?? m.assignees, milestone: p.milestone ?? m.milestone }))
  const ms = (milestones.data ?? []).find((m) => m.id === meta.milestone)
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-4 p-4">
      <div>
        <Link to="/$repo/issues" params={{ repo }} className="inline-flex items-center gap-1 text-[12px] text-fg-muted hover:text-fg">
          <ArrowLeft size={12} /> Issues
        </Link>
        <h2 className="mt-1 text-[15px] font-semibold">New issue</h2>
      </div>
      <form
        className="grid gap-6 md:grid-cols-[minmax(0,1fr)_240px]"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <div className="flex flex-col gap-3 rounded-md border border-border p-3">
          <Field label="Title">{(id) => <Input id={id} value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />}</Field>
          <MarkdownEditor
            value={body}
            onChange={setBody}
            repo={repo}
            label="Description"
            placeholder="What happened? Steps to reproduce, what you expected, versions…"
            minRows={8}
            onSubmit={() => title.trim() && create.mutate()}
          />
          {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
          <div className="flex justify-end">
            <Button type="submit" variant="primary" loading={create.isPending} disabled={!title.trim()}>
              Create issue
            </Button>
          </div>
        </div>
        <aside className="flex flex-col gap-3 text-[13px]">
          <MetaSections
            repo={repo}
            canEdit
            me={me.username}
            labels={(labels.data ?? []).filter((l) => meta.labels.includes(l.name))}
            assignees={(users.data ?? []).filter((u) => meta.assignees.includes(u.username))}
            milestone={ms ? { id: ms.id, title: ms.title, state: ms.state, due_on: ms.due_on } : null}
            onPatch={patch}
          />
        </aside>
      </form>
    </div>
  )
}
