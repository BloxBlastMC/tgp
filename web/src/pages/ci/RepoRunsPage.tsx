import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { AlertTriangle, Database, FileCode2, GitBranch, Layers, Play, SearchX, X, Zap } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Workflow } from '../../api/client'
import { branchesQuery, repoQuery, runsQuery, workflowsQuery } from '../../api/queries'
import { Pager, RunRows } from '../../components/ci/common'
import { EmptyState } from '../../components/Page'
import { Alert, Button, Checkbox, cx, Dialog, ErrorState, Eyebrow, Field, FilterBar, FilterMenu, FilterPicker, IconButton, Input, Select, SkeletonRows, useToast } from '../../components/ui'
import { canWrite as canWritePerm } from '../../lib/permissions'
import { statusLabel, type RunListSearch } from '../../lib/ci'
import { useRepoName } from '../repos/RepoLayout'

const PER_PAGE = 30

const statusOptions: { value: string; label: string }[] = [
  { value: 'active', label: 'Active' },
  { value: 'queued', label: statusLabel.queued },
  { value: 'in_progress', label: statusLabel.running },
  { value: 'waiting', label: statusLabel.waiting },
  { value: 'success', label: statusLabel.success },
  { value: 'failure', label: statusLabel.failure },
  { value: 'cancelled', label: statusLabel.cancelled },
  { value: 'timed_out', label: statusLabel.timed_out },
  { value: 'skipped', label: statusLabel.skipped },
]

export function RepoRunsPage() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const search = useSearch({ strict: false }) as RunListSearch
  const navigate = useNavigate()
  const set = (s: Partial<RunListSearch>) => void navigate({ to: '/$repo/ci', params: { repo: name }, search: { ...search, page: undefined, ...s } })
  const page = search.page ?? 1
  const runs = useQuery({
    ...runsQuery(name, { workflow: search.workflow, branch: search.branch, status: search.status, event: search.event, page, per_page: PER_PAGE }),
    refetchInterval: (q) => (q.state.data?.runs.some((r) => r.status !== 'completed') ? 5000 : 30_000),
    placeholderData: (prev) => prev,
  })
  const workflows = useQuery(workflowsQuery(name))
  const branches = useQuery(branchesQuery(name))
  const canWrite = canWritePerm(repo.permission)
  const [dispatch, setDispatch] = useState<Workflow | null>(null)
  const [errors, setErrors] = useState<Workflow | null>(null)
  const filtered = !!(search.workflow || search.branch || search.status || search.event)
  const selected = workflows.data?.find((w) => w.file === search.workflow)

  return (
    <div className="flex min-h-full">
      <aside className="w-60 shrink-0 border-r border-border bg-bg-sidebar/40 p-3" aria-label="Workflows">
        <Eyebrow as="h2" className="px-2 pb-2">
          Workflows
        </Eyebrow>
        <button
          type="button"
          onClick={() => set({ workflow: undefined })}
          className={cx('flex h-7 w-full items-center gap-2 rounded-md px-2 text-left transition-colors', !search.workflow ? 'bg-bg-active font-medium text-fg' : 'text-fg-muted hover:bg-bg-hover hover:text-fg')}
        >
          <Layers size={13} className="shrink-0 text-fg-faint" /> All workflows
        </button>
        {workflows.isPending ? (
          <SkeletonRows rows={2} />
        ) : workflows.isError ? (
          <p className="px-2 py-1 text-sm text-danger">{errorMessage(workflows.error)}</p>
        ) : workflows.data.length === 0 ? (
          <p className="px-2 py-2 text-sm text-fg-faint">
            No workflows. Add a YAML file under <code className="font-mono">.tgp/workflows/</code> on {repo.default_branch}.
          </p>
        ) : (
          workflows.data.map((w) => (
            <div key={w.path} className={cx('group flex items-center gap-1 rounded-md transition-colors', search.workflow === w.file ? 'bg-bg-active' : 'hover:bg-bg-hover')}>
              <button
                type="button"
                onClick={() => set({ workflow: w.file })}
                className={cx('flex h-7 min-w-0 flex-1 items-center gap-2 px-2 text-left', search.workflow === w.file ? 'font-medium text-fg' : 'text-fg-muted hover:text-fg')}
                title={w.path}
              >
                <FileCode2 size={13} className="shrink-0 text-fg-faint" />
                <span className="truncate">{w.name || w.file}</span>
              </button>
              {w.valid && w.triggers.includes('workflow_dispatch') && canWrite && (
                <IconButton
                  label={`Run ${w.file}`}
                  tooltip="Run workflow"
                  size="xs"
                  onClick={() => setDispatch(w)}
                  className="mr-0.5 opacity-0 group-focus-within:opacity-100 group-hover:opacity-100 focus-visible:opacity-100"
                >
                  <Play size={12} />
                </IconButton>
              )}
              {!w.valid && (
                <IconButton label={`${w.file} has errors`} tooltip="Show errors" size="xs" onClick={() => setErrors(w)} className="mr-0.5 text-danger hover:bg-danger/10 hover:text-danger">
                  <AlertTriangle size={13} />
                </IconButton>
              )}
            </div>
          ))
        )}
        <Eyebrow as="h2" className="mt-5 px-2 pb-2">
          Management
        </Eyebrow>
        <Link to="/$repo/ci/caches" params={{ repo: name }} search={{}} className="flex h-7 items-center gap-2 rounded-md px-2 text-fg-muted transition-colors hover:bg-bg-hover hover:text-fg">
          <Database size={13} className="text-fg-faint" /> Caches
        </Link>
      </aside>
      <div className="min-w-0 flex-1">
        <FilterBar
          className="px-5"
          end={
            <>
              {runs.data && <span className="text-sm text-fg-faint tabular-nums">{runs.data.total} runs</span>}
              {selected && selected.valid && selected.triggers.includes('workflow_dispatch') && canWrite && (
                <Button variant="primary" icon={<Play size={13} />} onClick={() => setDispatch(selected)}>
                  Run workflow
                </Button>
              )}
            </>
          }
        >
          <FilterMenu label="Status" value={search.status ?? ''} onChange={(v) => set({ status: v || undefined })} options={[{ value: '', label: 'Any' }, ...statusOptions]} />
          <FilterPicker
            title="Branch"
            icon={<GitBranch size={12} />}
            value={search.branch}
            onChange={(v) => set({ branch: v })}
            items={(branches.data ?? []).map((b) => ({ key: b.name, label: b.name }))}
          />
          <FilterMenu
            label="Event"
            icon={<Zap size={12} />}
            value={search.event ?? ''}
            onChange={(v) => set({ event: (v || undefined) as RunListSearch['event'] })}
            options={[
              { value: '', label: 'Any' },
              { value: 'push', label: 'Push' },
              { value: 'pull_request', label: 'Pull request' },
              { value: 'workflow_dispatch', label: 'Manual' },
              { value: 'release', label: 'Release' },
            ]}
          />
          {filtered && (
            <Button variant="ghost" size="sm" icon={<X size={12} />} onClick={() => void navigate({ to: '/$repo/ci', params: { repo: name }, search: {} })}>
              Clear filters
            </Button>
          )}
        </FilterBar>
        {selected && !selected.valid && (
          <div className="border-b border-border px-5 py-3">
            <WorkflowErrors workflow={selected} />
          </div>
        )}
        {runs.isPending ? (
          <SkeletonRows rows={6} />
        ) : runs.isError ? (
          <ErrorState error={runs.error} />
        ) : runs.data.runs.length === 0 ? (
          <EmptyState icon={filtered ? <SearchX size={20} /> : <Play size={20} />} title={filtered ? 'No matching runs' : 'No workflow runs yet'}>
            {!filtered && 'Runs appear here when a push, pull request or manual trigger starts a workflow.'}
          </EmptyState>
        ) : (
          <>
            <RunRows runs={runs.data.runs} />
            <Pager page={page} total={runs.data.total} perPage={PER_PAGE} onPage={(p) => void navigate({ to: '/$repo/ci', params: { repo: name }, search: { ...search, page: p > 1 ? p : undefined } })} />
          </>
        )}
      </div>
      {dispatch && <DispatchDialog repo={name} defaultRef={repo.default_branch} workflow={dispatch} onClose={() => setDispatch(null)} />}
      {errors && (
        <Dialog open onClose={() => setErrors(null)} title={`Errors in ${errors.file}`} width="max-w-2xl">
          <WorkflowErrors workflow={errors} />
        </Dialog>
      )}
    </div>
  )
}

export function WorkflowErrors({ workflow }: { workflow: Workflow }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-danger/30 bg-danger/8 px-3 py-2.5" role="alert" aria-label={`Errors in ${workflow.file}`}>
      <p className="flex items-center gap-1.5 text-base font-medium text-danger">
        <AlertTriangle size={13} /> {workflow.path} is invalid and won't run
      </p>
      <ul className="font-mono text-sm">
        {workflow.errors.map((e, i) => (
          <li key={i} className="flex gap-2">
            {e.line ? (
              <span className="shrink-0 text-fg-faint">
                {e.line}:{e.column ?? 0}
              </span>
            ) : null}
            <span className="break-words">{e.message}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function DispatchDialog({ repo, defaultRef, workflow, onClose }: { repo: string; defaultRef: string; workflow: Workflow; onClose: () => void }) {
  const navigate = useNavigate()
  const toast = useToast()
  const branches = useQuery(branchesQuery(repo))
  const [ref, setRef] = useState(defaultRef)
  const [inputs, setInputs] = useState<Record<string, string>>(() =>
    Object.fromEntries(workflow.dispatch_inputs.map((i) => [i.name, i.default ?? (i.type === 'boolean' ? 'false' : i.type === 'choice' ? (i.options?.[0] ?? '') : '')])),
  )
  const missing = workflow.dispatch_inputs.filter((i) => i.required && !inputs[i.name]?.trim()).map((i) => i.name)
  const run = useMutation({
    mutationFn: () => unwrap(api.POST('/repos/{repo}/workflows/{workflow}/dispatch', { params: { path: { repo, workflow: workflow.file } }, body: { ref, inputs } })),
    onSuccess: (r) => {
      onClose()
      toast.success(`Started run #${r.number}`)
      void navigate({ to: '/$repo/ci/runs/$number', params: { repo, number: String(r.number) }, search: {} })
    },
  })
  return (
    <Dialog
      open
      onClose={onClose}
      title={`Run ${workflow.name || workflow.file}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" icon={<Play size={13} />} loading={run.isPending} disabled={missing.length > 0 || !ref.trim()} onClick={() => run.mutate()}>
            Run workflow
          </Button>
        </>
      }
    >
      <Field label="Branch or tag">
        {(id) => (
          <>
            <Input id={id} value={ref} onChange={(e) => setRef(e.target.value)} list={`${id}-branches`} className="font-mono" />
            <datalist id={`${id}-branches`}>
              {(branches.data ?? []).map((b) => (
                <option key={b.name} value={b.name} />
              ))}
            </datalist>
          </>
        )}
      </Field>
      {workflow.dispatch_inputs.map((i) =>
        i.type === 'boolean' ? (
          <Checkbox key={i.name} checked={inputs[i.name] === 'true'} onChange={(v) => setInputs({ ...inputs, [i.name]: String(v) })} label={i.name} hint={i.description || undefined} />
        ) : (
          <Field key={i.name} label={`${i.name}${i.required ? ' *' : ''}`} hint={i.description || undefined}>
            {(id) =>
              i.type === 'choice' ? (
                <Select id={id} value={inputs[i.name] ?? ''} onChange={(e) => setInputs({ ...inputs, [i.name]: e.target.value })}>
                  {(i.options ?? []).map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </Select>
              ) : (
                <Input id={id} type={i.type === 'number' ? 'number' : 'text'} value={inputs[i.name] ?? ''} onChange={(e) => setInputs({ ...inputs, [i.name]: e.target.value })} />
              )
            }
          </Field>
        ),
      )}
      {run.isError && <Alert>{errorMessage(run.error)}</Alert>}
    </Dialog>
  )
}
