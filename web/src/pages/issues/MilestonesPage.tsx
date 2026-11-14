import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowLeft, Calendar, Plus } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Milestone } from '../../api/client'
import { milestonesQuery, repoQuery } from '../../api/queries'
import { EmptyState } from '../../components/Page'
import { Alert, Button, cx, Field, Input, Spinner, Time } from '../../components/ui'
import { MarkdownEditor } from '../../components/pulls/common'
import { isOverdue, milestoneProgress } from '../../lib/issues'
import { Segmented } from '../pulls/PullList'
import { useRepoName } from '../repos/RepoLayout'

export type MilestonesSearch = { state?: 'closed' }

export function validateMilestones(s: Record<string, unknown>): MilestonesSearch {
  return s.state === 'closed' ? { state: 'closed' } : {}
}

function useInvalidateMilestones(repo: string) {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'milestones'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'issues'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'issue'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'pull'] })
  }
}

function MilestoneForm({ repo, milestone, onDone }: { repo: string; milestone?: Milestone; onDone: () => void }) {
  const invalidate = useInvalidateMilestones(repo)
  const [title, setTitle] = useState(milestone?.title ?? '')
  const [due, setDue] = useState(milestone?.due_on ?? '')
  const [description, setDescription] = useState(milestone?.description ?? '')
  const save = useMutation({
    mutationFn: () =>
      milestone
        ? unwrap(
            api.PATCH('/repos/{repo}/milestones/{id}', {
              params: { path: { repo, id: milestone.id } },
              body: { title, description, ...(due ? { due_on: due } : { clear_due_on: true }) },
            }),
          )
        : unwrap(api.POST('/repos/{repo}/milestones', { params: { path: { repo } }, body: { title, description, due_on: due || undefined } })),
    onSuccess: () => {
      invalidate()
      onDone()
    },
  })
  return (
    <form
      className="flex flex-col gap-3 border-b border-border bg-bg-subtle px-4 py-3"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_180px]">
        <Field label="Title">{(id) => <Input id={id} value={title} onChange={(e) => setTitle(e.target.value)} autoFocus maxLength={100} />}</Field>
        <Field label="Due date (optional)">{(id) => <Input id={id} type="date" value={due} onChange={(e) => setDue(e.target.value)} />}</Field>
      </div>
      <MarkdownEditor value={description} onChange={setDescription} repo={repo} label="Milestone description" placeholder="What's in this milestone?" minRows={3} />
      {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" loading={save.isPending} disabled={!title.trim()}>
          {milestone ? 'Save' : 'Create milestone'}
        </Button>
      </div>
    </form>
  )
}

function MilestoneRow({ repo, m, canWrite, onEdit }: { repo: string; m: Milestone; canWrite: boolean; onEdit: () => void }) {
  const invalidate = useInvalidateMilestones(repo)
  const setState = useMutation({
    mutationFn: (state: 'open' | 'closed') => unwrap(api.PATCH('/repos/{repo}/milestones/{id}', { params: { path: { repo, id: m.id } }, body: { state } })),
    onSuccess: invalidate,
  })
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/repos/{repo}/milestones/{id}', { params: { path: { repo, id: m.id } } })),
    onSuccess: invalidate,
  })
  const pct = milestoneProgress(m)
  const overdue = isOverdue(m.due_on, m.state)
  return (
    <li className="grid gap-3 border-b border-border px-4 py-3 md:grid-cols-[minmax(0,1fr)_280px]" data-testid="milestone-row">
      <div className="min-w-0">
        <Link to="/$repo/issues" params={{ repo }} search={{ milestone: m.title, ...(m.state === 'closed' ? { state: 'closed' as const } : {}) }} className="text-lg font-semibold hover:text-accent">
          {m.title}
        </Link>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-3 text-sm text-fg-muted">
          {m.state === 'closed' ? (
            <span>
              Closed <Time iso={m.closed_at} />
            </span>
          ) : m.due_on ? (
            <span className={cx('flex items-center gap-1', overdue && 'text-danger')}>
              <Calendar size={12} /> {overdue ? 'Past due by' : 'Due'} {m.due_on}
            </span>
          ) : (
            <span className="text-fg-faint">No due date</span>
          )}
          <span className="text-fg-faint">
            Updated <Time iso={m.updated_at} />
          </span>
        </div>
        {m.description_html && <div className="markdown mt-1.5 text-fg-muted" dangerouslySetInnerHTML={{ __html: m.description_html }} />}
      </div>
      <div className="flex flex-col gap-1.5">
        <div className="h-2 overflow-hidden rounded-full bg-bg-hover" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`${m.title} progress`}>
          <div className="h-full rounded-full bg-success" style={{ width: `${pct}%` }} />
        </div>
        <div className="flex gap-3 text-sm text-fg-muted">
          <span className="font-medium text-fg">{pct}%</span>
          <span>{m.open_issues} open</span>
          <span>{m.closed_issues} closed</span>
        </div>
        {canWrite && (
          <div className="flex gap-1">
            <Button variant="ghost" onClick={onEdit}>
              Edit
            </Button>
            <Button variant="ghost" loading={setState.isPending} onClick={() => setState.mutate(m.state === 'open' ? 'closed' : 'open')}>
              {m.state === 'open' ? 'Close' : 'Reopen'}
            </Button>
            <Button
              variant="ghost"
              className="hover:text-danger"
              onClick={() => window.confirm(`Delete the milestone “${m.title}”? Its issues and pull requests stay, without a milestone.`) && del.mutate()}
            >
              Delete
            </Button>
          </div>
        )}
        {(setState.isError || del.isError) && <Alert>{errorMessage(setState.error ?? del.error)}</Alert>}
      </div>
    </li>
  )
}

export function MilestonesPage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const search = useSearch({ strict: false }) as MilestonesSearch
  const navigate = useNavigate()
  const state = search.state ?? 'open'
  const ms = useQuery(milestonesQuery(repo, state))
  const canWrite = r.permission === 'write' || r.permission === 'admin'
  const [editing, setEditing] = useState<number | 'new' | null>(null)
  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-2">
        <Link to="/$repo/issues" params={{ repo }} className="inline-flex items-center gap-1 text-sm text-fg-muted hover:text-fg">
          <ArrowLeft size={12} /> Issues
        </Link>
        <h2 className="font-semibold">Milestones</h2>
        <Segmented
          label="State"
          value={state}
          options={[
            { value: 'open', label: 'Open' },
            { value: 'closed', label: 'Closed' },
          ]}
          onChange={(s) => void navigate({ to: '/$repo/milestones', params: { repo }, search: s === 'closed' ? { state: 'closed' } : {} })}
        />
        <div className="flex-1" />
        {canWrite && editing !== 'new' && (
          <Button variant="primary" onClick={() => setEditing('new')}>
            <Plus size={14} /> New milestone
          </Button>
        )}
      </div>
      {editing === 'new' && <MilestoneForm repo={repo} onDone={() => setEditing(null)} />}
      {ms.isPending ? (
        <Spinner />
      ) : ms.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(ms.error)}</Alert>
        </div>
      ) : ms.data.length === 0 ? (
        <EmptyState title={`No ${state} milestones`}>{state === 'open' && canWrite && 'Group issues and pull requests into a release or sprint.'}</EmptyState>
      ) : (
        <ul>
          {ms.data.map((m) =>
            editing === m.id ? (
              <li key={m.id}>
                <MilestoneForm repo={repo} milestone={m} onDone={() => setEditing(null)} />
              </li>
            ) : (
              <MilestoneRow key={m.id} repo={repo} m={m} canWrite={canWrite} onEdit={() => setEditing(m.id)} />
            ),
          )}
        </ul>
      )}
    </div>
  )
}
