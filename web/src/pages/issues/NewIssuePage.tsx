import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { CircleDot, Lock } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { labelsQuery, milestonesQuery, repoQuery, sessionQuery, usersQuery } from '../../api/queries'
import { MetaSections, type MetaPatch } from '../../components/issues/meta'
import { EmptyState, SubHeader } from '../../components/Page'
import { MarkdownEditor } from '../../components/pulls/common'
import { Alert, Button, Card, Field, Input, useToast } from '../../components/ui'
import { canTriage } from '../../lib/permissions'
import { useRepoName } from '../repos/RepoLayout'

export function NewIssuePage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const me = useQuery(sessionQuery).data!.user
  const navigate = useNavigate()
  const qc = useQueryClient()
  const toast = useToast()
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
      toast.success(`Opened issue #${is.number}`)
      void navigate({ to: '/$repo/issues/$number', params: { repo, number: String(is.number) } })
    },
  })
  if (!canTriage(r.permission))
    return (
      <EmptyState icon={<Lock size={20} />} title="Can't open issues">
        You need at least triage access to {repo}.
      </EmptyState>
    )
  const patch = (p: MetaPatch) =>
    setMeta((m) => ({ labels: p.labels ?? m.labels, assignees: p.assignees ?? m.assignees, milestone: p.milestone ?? m.milestone }))
  const ms = (milestones.data ?? []).find((m) => m.id === meta.milestone)
  return (
    <div className="mx-auto flex max-w-6xl flex-col px-6 py-5">
      <SubHeader back={{ to: '/$repo/issues', params: { repo }, label: 'Issues' }} title="New issue" description={`Report a bug or suggest an idea for ${repo}.`} />
      <form
        className="grid gap-6 md:grid-cols-[minmax(0,1fr)_256px]"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Card
          footer={
            <Button type="submit" variant="primary" loading={create.isPending} disabled={!title.trim()} icon={<CircleDot size={14} />}>
              Create issue
            </Button>
          }
        >
          <div className="flex flex-col gap-4">
            <Field label="Title" required>
              {(id) => <Input id={id} value={title} onChange={(e) => setTitle(e.target.value)} autoFocus size="lg" placeholder="A short, specific summary" />}
            </Field>
            <MarkdownEditor
              value={body}
              onChange={setBody}
              repo={repo}
              label="Description"
              placeholder="What happened? Steps to reproduce, what you expected, versions…"
              minRows={10}
              onSubmit={() => title.trim() && create.mutate()}
            />
            {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
          </div>
        </Card>
        <aside className="flex flex-col gap-4 text-base">
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
