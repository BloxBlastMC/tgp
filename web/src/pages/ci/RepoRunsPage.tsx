import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { AlertTriangle, Database, FileCode2, Play, X } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { api, errorMessage, unwrap, type Workflow } from '../../api/client'
import { branchesQuery, repoQuery, runsQuery, workflowsQuery } from '../../api/queries'
import { Pager, RunRows } from '../../components/ci/common'
import { EmptyState } from '../../components/Page'
import { Alert, Button, Checkbox, cx, Dialog, Field, Input, Select, Spinner } from '../../components/ui'
import { statusLabel, type RunListSearch } from '../../lib/ci'
import { useRepoName } from '../repos/RepoLayout'

const PER_PAGE = 30

function FilterSelect({ label, value, onChange, width, children }: { label: string; value: string; onChange: (v: string) => void; width: string; children: ReactNode }) {
  return (
    <div className={cx('shrink-0', width)}>
      <Select value={value} onChange={(e) => onChange(e.target.value)} aria-label={label} className="h-7 text-[12px]">
        {children}
      </Select>
    </div>
  )
}

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
  })
  const workflows = useQuery(workflowsQuery(name))
  const branches = useQuery(branchesQuery(name))
  const canWrite = repo.permission === 'write' || repo.permission === 'admin'
  const [dispatch, setDispatch] = useState<Workflow | null>(null)
  const [errors, setErrors] = useState<Workflow | null>(null)
  const filtered = !!(search.workflow || search.branch || search.status || search.event)
  const selected = workflows.data?.find((w) => w.file === search.workflow)

  return (
    <div className="flex min-h-full">
      <aside className="w-60 shrink-0 border-r border-border p-2" aria-label="Workflows">
        <h2 className="px-2 py-1 text-[11px] font-semibold tracking-wide text-fg-faint uppercase">Workflows</h2>
        <button
          type="button"
          onClick={() => set({ workflow: undefined })}
          className={cx('flex h-7 w-full items-center gap-2 rounded px-2 text-left', !search.workflow ? 'bg-bg-hover text-fg' : 'text-fg-muted hover:bg-bg-hover hover:text-fg')}
        >
          All workflows
        </button>
        {workflows.isPending ? (
          <Spinner />
        ) : workflows.isError ? (
          <p className="px-2 py-1 text-[12px] text-danger">{errorMessage(workflows.error)}</p>
        ) : workflows.data.length === 0 ? (
          <p className="px-2 py-2 text-[12px] text-fg-faint">
            No workflows. Add a YAML file under <code className="font-mono">.tgp/workflows/</code> on {repo.default_branch}.
          </p>
        ) : (
          workflows.data.map((w) => (
            <div key={w.path} className={cx('group flex items-center gap-1 rounded', search.workflow === w.file ? 'bg-bg-hover' : 'hover:bg-bg-hover')}>
              <button type="button" onClick={() => set({ workflow: w.file })} className={cx('flex h-7 min-w-0 flex-1 items-center gap-2 px-2 text-left', search.workflow === w.file ? 'text-fg' : 'text-fg-muted hover:text-fg')} title={w.path}>
                <FileCode2 size={13} className="shrink-0" />
                <span className="truncate">{w.name || w.file}</span>
              </button>
              {w.valid && w.triggers.includes('workflow_dispatch') && canWrite && (
                <button type="button" onClick={() => setDispatch(w)} className="mr-1 hidden rounded p-0.5 text-fg-muted group-hover:block hover:bg-bg-elevated hover:text-fg" aria-label={`Run ${w.file}`} title="Run workflow">
                  <Play size={12} />
                </button>
              )}
              {!w.valid && (
                <button type="button" onClick={() => setErrors(w)} className="mr-1 rounded p-0.5 text-danger hover:bg-danger/10" aria-label={`${w.file} has errors`} title="Show errors">
                  <AlertTriangle size={13} />
                </button>
              )}
            </div>
          ))
        )}
        <h2 className="mt-3 px-2 py-1 text-[11px] font-semibold tracking-wide text-fg-faint uppercase">Management</h2>
        <Link to="/$repo/ci/caches" params={{ repo: name }} search={{}} className="flex h-7 items-center gap-2 rounded px-2 text-fg-muted hover:bg-bg-hover hover:text-fg">
          <Database size={13} /> Caches
        </Link>
      </aside>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
          <FilterSelect label="Status" width="w-32" value={search.status ?? ''} onChange={(v) => set({ status: v || undefined })}>
            <option value="">Any status</option>
            {statusOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </FilterSelect>
          <FilterSelect label="Branch" width="w-40" value={search.branch ?? ''} onChange={(v) => set({ branch: v || undefined })}>
            <option value="">Any branch</option>
            {(branches.data ?? []).map((b) => (
              <option key={b.name} value={b.name}>
                {b.name}
              </option>
            ))}
          </FilterSelect>
          <FilterSelect label="Event" width="w-36" value={search.event ?? ''} onChange={(v) => set({ event: (v || undefined) as RunListSearch['event'] })}>
            <option value="">Any event</option>
            <option value="push">Push</option>
            <option value="pull_request">Pull request</option>
            <option value="workflow_dispatch">Manual</option>
            <option value="release">Release</option>
          </FilterSelect>
          {filtered && (
            <button type="button" className="flex items-center gap-1 text-[12px] text-fg-muted hover:text-fg" onClick={() => void navigate({ to: '/$repo/ci', params: { repo: name }, search: {} })}>
              <X size={12} /> Clear filters
            </button>
          )}
          <div className="flex-1" />
          {runs.data && <span className="text-[12px] text-fg-faint">{runs.data.total} runs</span>}
          {selected && selected.valid && selected.triggers.includes('workflow_dispatch') && canWrite && (
            <Button variant="primary" onClick={() => setDispatch(selected)}>
              <Play size={13} /> Run workflow
            </Button>
          )}
        </div>
        {selected && !selected.valid && (
          <div className="border-b border-border px-4 py-2">
            <WorkflowErrors workflow={selected} />
          </div>
        )}
        {runs.isPending ? (
          <Spinner />
        ) : runs.isError ? (
          <div className="p-4">
            <Alert>{errorMessage(runs.error)}</Alert>
          </div>
        ) : runs.data.runs.length === 0 ? (
          <EmptyState title={filtered ? 'No matching runs' : 'No workflow runs yet'}>
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
    <div className="flex flex-col gap-1" role="alert" aria-label={`Errors in ${workflow.file}`}>
      <p className="flex items-center gap-1.5 text-[13px] font-medium text-danger">
        <AlertTriangle size={13} /> {workflow.path} is invalid and won't run
      </p>
      <ul className="font-mono text-[12px]">
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
          <Button variant="primary" loading={run.isPending} disabled={missing.length > 0 || !ref.trim()} onClick={() => run.mutate()}>
            <Play size={13} /> Run workflow
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
