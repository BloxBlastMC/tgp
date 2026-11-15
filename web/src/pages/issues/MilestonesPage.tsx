import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Calendar, CheckCircle2, Milestone as MilestoneIcon, MoreHorizontal, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Milestone } from '../../api/client'
import { milestonesQuery, repoQuery } from '../../api/queries'
import { EmptyState, SubHeader, Container } from '../../components/Page'
import { MarkdownEditor } from '../../components/pulls/common'
import { Alert, Button, cx, Dialog, ErrorState, Field, IconButton, Input, List, Menu, MenuItem, MenuSeparator, Meter, SegmentedControl, SkeletonRows, Time, useConfirm, useToast } from '../../components/ui'
import { dueLabel } from '../../lib/format'
import { milestoneProgress } from '../../lib/issues'
import { canWrite as canWritePerm } from '../../lib/permissions'
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

function MilestoneDialog({ repo, milestone, onDone }: { repo: string; milestone?: Milestone; onDone: () => void }) {
  const invalidate = useInvalidateMilestones(repo)
  const toast = useToast()
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
      toast.success(milestone ? 'Milestone updated' : 'Milestone created')
      onDone()
    },
  })
  return (
    <Dialog open onClose={onDone} title={milestone ? 'Edit milestone' : 'New milestone'} width="max-w-xl">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate()
        }}
      >
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_180px]">
          <Field label="Title">{(id) => <Input id={id} value={title} onChange={(e) => setTitle(e.target.value)} autoFocus maxLength={100} placeholder="v1.5" />}</Field>
          <Field label="Due date (optional)">{(id) => <Input id={id} type="date" value={due} onChange={(e) => setDue(e.target.value)} />}</Field>
        </div>
        <MarkdownEditor value={description} onChange={setDescription} repo={repo} label="Milestone description" placeholder="What's in this milestone?" minRows={4} />
        {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={save.isPending} disabled={!title.trim()}>
            {milestone ? 'Save' : 'Create milestone'}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

function MilestoneRow({ repo, m, canWrite, onEdit }: { repo: string; m: Milestone; canWrite: boolean; onEdit: () => void }) {
  const invalidate = useInvalidateMilestones(repo)
  const confirm = useConfirm()
  const toast = useToast()
  const setState = useMutation({
    mutationFn: (state: 'open' | 'closed') => unwrap(api.PATCH('/repos/{repo}/milestones/{id}', { params: { path: { repo, id: m.id } }, body: { state } })),
    onSuccess: (_, state) => {
      toast.success(state === 'closed' ? `Closed ${m.title}` : `Reopened ${m.title}`)
      invalidate()
    },
  })
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/repos/{repo}/milestones/{id}', { params: { path: { repo, id: m.id } } })),
    onSuccess: () => {
      toast.success(`Deleted ${m.title}`)
      invalidate()
    },
  })
  const pct = milestoneProgress(m)
  const due = dueLabel(m.due_on, m.state)
  return (
    <li className="grid gap-4 border-b border-border px-5 py-4 last:border-0 md:grid-cols-[minmax(0,1fr)_300px]" data-testid="milestone-row">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          {m.state === 'closed' ? <CheckCircle2 size={16} className="shrink-0 text-done" /> : <MilestoneIcon size={16} className="shrink-0 text-fg-muted" />}
          <Link
            to="/$repo/issues"
            params={{ repo }}
            search={{ milestone: m.title, ...(m.state === 'closed' ? { state: 'closed' as const } : {}) }}
            className="text-lg font-semibold tracking-tight hover:text-accent"
          >
            {m.title}
          </Link>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-fg-muted">
          {m.state === 'closed' ? (
            <span>
              Closed <Time iso={m.closed_at} />
            </span>
          ) : due ? (
            <span className={cx('flex items-center gap-1', due.overdue && 'font-medium text-danger')} title={m.due_on ?? undefined}>
              <Calendar size={12} /> {due.text}
            </span>
          ) : (
            <span className="text-fg-faint">No due date</span>
          )}
        </div>
        {m.description_html && <div className="markdown mt-2 text-sm text-fg-muted" dangerouslySetInnerHTML={{ __html: m.description_html }} />}
      </div>
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-3">
          <Meter value={pct} label={`${m.title} progress`} tone={pct === 100 ? 'success' : 'accent'} className="flex-1" />
          {canWrite && (
            <Menu
              placement="bottom-end"
              label={`${m.title} actions`}
              trigger={(t) => (
                <IconButton {...t} label={`Actions for ${m.title}`} size="sm" loading={setState.isPending || del.isPending}>
                  <MoreHorizontal size={14} />
                </IconButton>
              )}
            >
              <MenuItem icon={<Pencil size={13} />} onSelect={onEdit}>
                Edit
              </MenuItem>
              <MenuItem icon={m.state === 'open' ? <CheckCircle2 size={13} /> : <RefreshCw size={13} />} onSelect={() => setState.mutate(m.state === 'open' ? 'closed' : 'open')}>
                {m.state === 'open' ? 'Close milestone' : 'Reopen milestone'}
              </MenuItem>
              <MenuSeparator />
              <MenuItem
                icon={<Trash2 size={13} />}
                tone="danger"
                onSelect={async () => {
                  const ok = await confirm({
                    title: `Delete the milestone “${m.title}”?`,
                    description: 'Its issues and pull requests stay, without a milestone.',
                    confirmLabel: 'Delete milestone',
                  })
                  if (ok) del.mutate()
                }}
              >
                Delete
              </MenuItem>
            </Menu>
          )}
        </div>
        {m.open_issues + m.closed_issues === 0 ? (
          <p className="text-sm text-fg-faint">No issues yet</p>
        ) : (
          <div className="flex gap-3 text-sm text-fg-muted tabular-nums">
            <span className="font-semibold text-fg">{pct}% complete</span>
            <span>{m.open_issues} open</span>
            <span>{m.closed_issues} closed</span>
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
  const canWrite = canWritePerm(r.permission)
  const [editing, setEditing] = useState<Milestone | 'new' | null>(null)
  return (
    <Container width="wide" className="flex flex-col">
      <SubHeader
        back={{ to: '/$repo/issues', params: { repo }, label: 'Issues' }}
        title="Milestones"
        description="Group issues and pull requests into a release or sprint and track progress."
        actions={
          canWrite && (
            <Button variant="primary" icon={<Plus size={14} />} onClick={() => setEditing('new')}>
              New milestone
            </Button>
          )
        }
      />
      <div className="mb-3">
        <SegmentedControl
          label="State"
          value={state}
          options={[
            { value: 'open', label: 'Open' },
            { value: 'closed', label: 'Closed' },
          ]}
          onChange={(s) => void navigate({ to: '/$repo/milestones', params: { repo }, search: s === 'closed' ? { state: 'closed' } : {} })}
        />
      </div>
      {ms.isPending ? (
        <List>
          <SkeletonRows rows={3} />
        </List>
      ) : ms.isError ? (
        <ErrorState error={ms.error} className="p-0" />
      ) : ms.data.length === 0 ? (
        <EmptyState size="inline" icon={<MilestoneIcon size={18} />} title={`No ${state} milestones`} action={state === 'open' && canWrite && <Button onClick={() => setEditing('new')}>Create a milestone</Button>}>
          {state === 'open' && 'Group issues and pull requests into a release or sprint.'}
        </EmptyState>
      ) : (
        <List>
          {ms.data.map((m) => (
            <MilestoneRow key={m.id} repo={repo} m={m} canWrite={canWrite} onEdit={() => setEditing(m)} />
          ))}
        </List>
      )}
      {editing && <MilestoneDialog key={editing === 'new' ? 'new' : editing.id} repo={repo} milestone={editing === 'new' ? undefined : editing} onDone={() => setEditing(null)} />}
    </Container>
  )
}
