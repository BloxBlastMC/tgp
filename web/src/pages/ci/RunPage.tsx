import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { Ban, ChevronDown, Download, Trash2, ChevronRight, Cpu, GitCommit, HardDrive, RotateCw, Server, Timer, Box } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api, ApiRequestError, errorMessage, unwrap, type Job, type WorkflowRunDetail } from '../../api/client'
import { repoQuery, runArtifactsQuery, runQuery } from '../../api/queries'
import { Duration, EventBadge, eventLabel, KindIcon, StatusIcon } from '../../components/ci/common'
import { StepLog, useJobLogs } from '../../components/ci/logs'
import { EmptyState } from '../../components/Page'
import { Alert, Badge, Button, cx, Spinner, Time } from '../../components/ui'
import { defaultJob, formatBytes, groupJobs, groupKind, isFailed, linesByStep, statusKind, statusLabel, statusTone, stepTitle } from '../../lib/ci'
import { useRepoName } from '../repos/RepoLayout'
import { artifactZipUrl, expiresText } from '../../lib/releases'

export function RunPage() {
  const repo = useRepoName()
  const { number } = useParams({ strict: false }) as { number: string }
  const n = Number(number)
  const search = useSearch({ strict: false }) as { job?: number }
  const run = useQuery({ ...runQuery(repo, n), refetchInterval: (q) => (q.state.data && q.state.data.status !== 'completed' ? 3000 : false) })

  if (run.isPending) return <Spinner />
  if (run.isError) {
    if (run.error instanceof ApiRequestError && run.error.status === 404) return <EmptyState title="Run not found">There's no run #{number} in {repo}.</EmptyState>
    return (
      <div className="p-4">
        <Alert>{errorMessage(run.error)}</Alert>
      </div>
    )
  }
  const r = run.data
  const job = r.jobs.find((j) => j.id === search.job) ?? defaultJob(r.jobs)
  return (
    <div className="flex h-full min-h-0 flex-col">
      <RunHeader repo={repo} run={r} />
      <div className="flex min-h-0 flex-1">
        <div className="flex w-64 shrink-0 flex-col overflow-auto border-r border-border">
          <JobList repo={repo} run={r} selected={job?.id} />
          <ArtifactList repo={repo} run={r} />
        </div>
        <div className="min-w-0 flex-1">{job ? <JobPanel key={job.id} repo={repo} run={r} job={job} /> : <EmptyState title="No jobs">This run has no jobs.</EmptyState>}</div>
      </div>
    </div>
  )
}

function useRunMutations(repo: string, run: WorkflowRunDetail) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const done = (r: WorkflowRunDetail) => {
    qc.setQueryData(runQuery(repo, run.number).queryKey, r)
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'runs'] })
    void navigate({ to: '/$repo/ci/runs/$number', params: { repo, number: String(r.number) }, search: {}, replace: true })
  }
  const rerun = useMutation({
    mutationFn: (failedOnly: boolean) => unwrap(api.POST('/repos/{repo}/runs/{number}/rerun', { params: { path: { repo, number: run.number } }, body: { failed_only: failedOnly } })),
    onSuccess: done,
  })
  const cancel = useMutation({
    mutationFn: () => unwrap(api.POST('/repos/{repo}/runs/{number}/cancel', { params: { path: { repo, number: run.number } } })),
    onSuccess: (r) => qc.setQueryData(runQuery(repo, run.number).queryKey, r),
  })
  const rerunJob = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/repos/{repo}/jobs/{id}/rerun', { params: { path: { repo, id } } })),
    onSuccess: (r, id) => {
      qc.setQueryData(runQuery(repo, run.number).queryKey, r)
      void qc.invalidateQueries({ queryKey: ['repo', repo, 'runs'] })
      const same = r.jobs.find((j) => j.id === id) ?? r.jobs.find((j) => j.name === run.jobs.find((x) => x.id === id)?.name)
      void navigate({ to: '/$repo/ci/runs/$number', params: { repo, number: String(r.number) }, search: same ? { job: same.id } : {}, replace: true })
    },
  })
  return { rerun, cancel, rerunJob }
}

function RunHeader({ repo, run }: { repo: string; run: WorkflowRunDetail }) {
  const { rerun, cancel } = useRunMutations(repo, run)
  const kind = statusKind(run.status, run.conclusion)
  const anyFailed = run.jobs.some((j) => j.status === 'completed' && isFailed(j.conclusion))
  const err = rerun.error ?? cancel.error
  return (
    <div className="shrink-0 border-b border-border px-4 py-3">
      <div className="flex items-start gap-3">
        <KindIcon kind={kind} size={20} className="mt-0.5" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-[15px] font-semibold">{run.title || run.workflow_name}</h1>
            <span className="text-fg-faint">#{run.number}</span>
            <Badge tone={statusTone[kind]}>{statusLabel[kind]}</Badge>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-fg-muted">
            <Link to="/$repo/ci" params={{ repo }} search={{ workflow: run.workflow_path.split('/').pop() }} className="hover:text-fg" title={run.workflow_path}>
              {run.workflow_name}
            </Link>
            <span>{eventLabel[run.event] ?? run.event}</span>
            {run.pull_number ? (
              <Link to="/$repo/pulls/$number" params={{ repo, number: String(run.pull_number) }} search={{}} className="hover:text-fg">
                <EventBadge run={run} />
              </Link>
            ) : (
              <EventBadge run={run} />
            )}
            <Link to="/$repo/commit/$sha" params={{ repo, sha: run.head_sha }} className="inline-flex items-center gap-1 font-mono hover:text-fg">
              <GitCommit size={11} />
              {run.head_sha.slice(0, 7)}
            </Link>
            {run.actor && <span>by {run.actor.display_name}</span>}
            <span>
              created <Time iso={run.created_at} />
            </span>
            <Duration start={run.started_at} end={run.completed_at} />
            {run.attempt > 1 && <span>attempt {run.attempt}</span>}
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          {run.can_cancel && (
            <Button variant="danger" loading={cancel.isPending} onClick={() => window.confirm('Cancel this run?') && cancel.mutate()}>
              <Ban size={13} /> Cancel run
            </Button>
          )}
          {run.can_rerun && anyFailed && (
            <Button loading={rerun.isPending && rerun.variables === true} onClick={() => rerun.mutate(true)}>
              <RotateCw size={13} /> Re-run failed jobs
            </Button>
          )}
          {run.can_rerun && (
            <Button loading={rerun.isPending && rerun.variables === false} onClick={() => rerun.mutate(false)}>
              <RotateCw size={13} /> Re-run all jobs
            </Button>
          )}
        </div>
      </div>
      {run.status_reason && (
        <div className="mt-2">
          <Alert tone="warning">{run.status_reason}</Alert>
        </div>
      )}
      {err && (
        <div className="mt-2">
          <Alert>{errorMessage(err)}</Alert>
        </div>
      )}
    </div>
  )
}

function JobRow({ repo, run, job, selected, nested }: { repo: string; run: WorkflowRunDetail; job: Job; selected: boolean; nested?: boolean }) {
  return (
    <Link
      to="/$repo/ci/runs/$number"
      params={{ repo, number: String(run.number) }}
      search={{ job: job.id }}
      replace
      className={cx('flex h-8 items-center gap-2 rounded pr-2 text-[13px]', nested ? 'pl-6' : 'pl-2', selected ? 'bg-bg-hover text-fg' : 'text-fg-muted hover:bg-bg-hover hover:text-fg')}
      aria-current={selected ? 'page' : undefined}
    >
      <StatusIcon status={job.status} conclusion={job.conclusion} size={14} />
      <span className="min-w-0 flex-1 truncate">{nested && job.matrix ? matrixText(job) : job.name}</span>
      <span className="shrink-0 text-[11px] text-fg-faint">
        <Duration start={job.started_at} end={job.completed_at} />
      </span>
    </Link>
  )
}

/** "1.27, linux" from a matrix job's name suffix, else the name. */
function matrixText(job: Job): string {
  const m = job.name.match(/\(([^()]*)\)\s*$/)
  return m ? m[1]! : job.name
}

function JobList({ repo, run, selected }: { repo: string; run: WorkflowRunDetail; selected?: number }) {
  const groups = groupJobs(run.jobs)
  const [closed, setClosed] = useState<Record<string, boolean>>({})
  return (
    <nav className="p-2" aria-label="Jobs">
      <h2 className="px-2 py-1 text-[11px] font-semibold tracking-wide text-fg-faint uppercase">Jobs</h2>
      {groups.map((g) =>
        g.jobs.length === 1 && !g.jobs[0]!.matrix ? (
          <JobRow key={g.key} repo={repo} run={run} job={g.jobs[0]!} selected={g.jobs[0]!.id === selected} />
        ) : (
          <div key={g.key}>
            <button type="button" onClick={() => setClosed((c) => ({ ...c, [g.key]: !c[g.key] }))} className="flex h-8 w-full items-center gap-2 rounded px-2 text-left text-[13px] text-fg-muted hover:bg-bg-hover hover:text-fg" aria-expanded={!closed[g.key]}>
              <KindIcon kind={groupKind(g.jobs)} size={14} />
              <span className="min-w-0 flex-1 truncate font-medium">{g.key}</span>
              <span className="text-[11px] text-fg-faint">{g.jobs.length}</span>
              {closed[g.key] ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
            </button>
            {!closed[g.key] && g.jobs.map((j) => <JobRow key={j.id} repo={repo} run={run} job={j} selected={j.id === selected} nested />)}
          </div>
        ),
      )}
    </nav>
  )
}

function JobPanel({ repo, run, job }: { repo: string; run: WorkflowRunDetail; job: Job }) {
  const qc = useQueryClient()
  const { rerunJob } = useRunMutations(repo, run)
  const logs = useJobLogs(repo, job, (j) => {
    qc.setQueryData(runQuery(repo, run.number).queryKey, (r: WorkflowRunDetail | undefined) => (r ? { ...r, jobs: r.jobs.map((x) => (x.id === j.id ? j : x)) } : r))
    if (j.status === 'completed') void qc.invalidateQueries({ queryKey: runQuery(repo, run.number).queryKey })
  })
  const byStep = linesByStep(logs.lines)
  const kind = statusKind(job.status, job.conclusion)
  const steps = [
    ...(byStep.has(0) ? [{ number: 0, name: 'Set up job', status: job.status === 'in_progress' && job.steps.every((s) => s.status === 'pending') ? 'in_progress' : 'completed', conclusion: null as string | null, started_at: job.started_at, completed_at: job.steps[0]?.started_at ?? null }] : []),
    ...job.steps.map((s) => ({ ...s, conclusion: s.conclusion ?? null })),
    ...(byStep.has(-1) ? [{ number: -1, name: 'Complete job', status: job.status === 'completed' ? 'completed' : 'in_progress', conclusion: null as string | null, started_at: null, completed_at: job.completed_at }] : []),
  ]
  // Step numbers seen in the log but missing from the step list (shouldn't happen, but don't lose lines).
  for (const k of byStep.keys()) if (!steps.some((s) => s.number === k)) steps.push({ number: k, name: stepTitle(k, job.steps), status: 'completed', conclusion: null, started_at: null, completed_at: null })

  // Auto-scroll while following a running job, unless the user scrolled up.
  const pane = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  useLayoutEffect(() => {
    const el = pane.current
    if (el && stick.current && job.status === 'in_progress') el.scrollTop = el.scrollHeight
  }, [logs.lines.length, job.status])
  // Jump to #L<n> once lines have loaded.
  const jumped = useRef(false)
  useEffect(() => {
    if (jumped.current || !logs.lines.length) return
    const m = window.location.hash.match(/^#L(\d+)$/)
    if (!m) return
    jumped.current = true
    window.setTimeout(() => document.getElementById(`L${m[1]}`)?.scrollIntoView({ block: 'center' }), 50)
  }, [logs.lines.length])

  return (
    <div
      ref={pane}
      className="h-full overflow-auto"
      onScroll={(e) => {
        const el = e.currentTarget
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
      }}
    >
      <div className="sticky top-0 z-10 border-b border-border bg-bg px-4 py-2.5">
        <div className="flex items-center gap-2">
          <KindIcon kind={kind} size={16} />
          <h2 className="min-w-0 flex-1 truncate font-medium">{job.name}</h2>
          <span className="text-[12px] text-fg-faint">
            <Duration start={job.started_at} end={job.completed_at} />
          </span>
          {logs.live && <Badge tone="accent">live</Badge>}
          {run.can_rerun && job.status === 'completed' && (
            <Button variant="ghost" loading={rerunJob.isPending} onClick={() => rerunJob.mutate(job.id)} aria-label={`Re-run ${job.name}`}>
              <RotateCw size={13} /> Re-run job
            </Button>
          )}
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-fg-faint">
          <span className="inline-flex items-center gap-1" title="Runner">
            <Server size={11} /> {job.runner?.name ?? 'no runner yet'}
          </span>
          <span className="inline-flex items-center gap-1 font-mono" title="Image">
            <Box size={11} /> {job.image}
          </span>
          <span className="inline-flex items-center gap-1" title="CPU limit">
            <Cpu size={11} /> {job.resources.cpus} CPU
          </span>
          <span className="inline-flex items-center gap-1" title="Memory limit">
            <HardDrive size={11} /> {formatBytes(job.resources.memory_bytes)}
          </span>
          <span title="Process limit">{job.resources.pids} pids</span>
          <span className="inline-flex items-center gap-1" title="Timeout">
            <Timer size={11} /> {job.timeout_minutes}m timeout
          </span>
          {job.labels.length > 0 && <span title="Runner labels (runs-on)">runs-on: {job.labels.join(', ')}</span>}
          {job.attempt > 1 && <span>attempt {job.attempt}</span>}
        </div>
        {rerunJob.isError && (
          <div className="mt-2">
            <Alert>{errorMessage(rerunJob.error)}</Alert>
          </div>
        )}
      </div>
      <div className="flex flex-col gap-2 p-3">
        {job.failure_reason && <Alert>{job.failure_reason}</Alert>}
        {job.status === 'queued' && <Alert tone="warning">{job.wait_reason ? `Waiting: ${job.wait_reason}` : 'Waiting for a runner to pick up this job…'}</Alert>}
        {job.status === 'pending' && <Alert tone="info">{job.wait_reason ?? (job.needs.length ? `Waiting for ${job.needs.join(', ')} to finish.` : 'Waiting to start.')}</Alert>}
        {logs.error && !logs.live && <Alert tone="warning">Couldn't load logs: {logs.error}</Alert>}
        {logs.truncated && <Alert tone="warning">The log reached the size limit; later output was dropped.</Alert>}
        <ul className="flex flex-col" aria-label="Steps">
          {steps.map((s) => (
            <StepSection key={s.number} step={s} lines={byStep.get(s.number) ?? []} jobRunning={job.status === 'in_progress'} />
          ))}
        </ul>
        {steps.length === 0 && job.status === 'completed' && <p className="px-2 text-fg-faint">This job produced no output.</p>}
      </div>
    </div>
  )
}

type StepView = { number: number; name: string; status: string; conclusion: string | null; started_at?: string | null; completed_at?: string | null }

function StepSection({ step, lines, jobRunning }: { step: StepView; lines: { n: number; step: number; t: string; text: string }[]; jobRunning: boolean }) {
  const auto = step.status === 'in_progress' || step.conclusion === 'failure' || (jobRunning && step.number === 0 && step.status === 'in_progress')
  const [open, setOpen] = useState<boolean | null>(null)
  const isOpen = open ?? auto
  return (
    <li className="rounded" data-testid="step">
      <button type="button" onClick={() => setOpen(!isOpen)} className="flex h-8 w-full items-center gap-2 rounded px-2 text-left hover:bg-bg-hover" aria-expanded={isOpen}>
        {isOpen ? <ChevronDown size={13} className="text-fg-faint" /> : <ChevronRight size={13} className="text-fg-faint" />}
        <StatusIcon status={step.status} conclusion={step.conclusion ?? (step.number <= 0 && step.status === 'completed' ? 'success' : null)} size={14} />
        <span className="min-w-0 flex-1 truncate">{step.name}</span>
        <span className="text-[11px] text-fg-faint">
          <Duration start={step.started_at} end={step.completed_at} />
        </span>
      </button>
      {isOpen && (
        <div className="ml-2 border-l border-border py-1 pl-1">
          {lines.length ? <StepLog lines={lines} /> : <p className="px-3 py-1 font-mono text-[12px] text-fg-faint">{step.status === 'pending' ? 'Not started.' : step.conclusion === 'skipped' ? 'Skipped.' : 'No output.'}</p>}
        </div>
      )}
    </li>
  )
}

function ArtifactList({ repo, run }: { repo: string; run: WorkflowRunDetail }) {
  const qc = useQueryClient()
  const perm = useQuery(repoQuery(repo)).data?.permission
  const canWrite = perm === 'write' || perm === 'admin'
  // Jobs upload artifacts while the run is going: poll until it's done.
  const q = useQuery({ ...runArtifactsQuery(repo, run.number), refetchInterval: run.status !== 'completed' ? 5000 : false })
  const del = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/repos/{repo}/artifacts/{id}', { params: { path: { repo, id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: runArtifactsQuery(repo, run.number).queryKey }),
  })
  const list = q.data ?? []
  if (list.length === 0 && !q.isError) return null
  return (
    <section className="border-t border-border p-2" aria-label="Artifacts">
      <h2 className="px-2 py-1 text-[11px] font-semibold tracking-wide text-fg-faint uppercase">
        Artifacts <span className="font-normal">{list.length}</span>
      </h2>
      {q.isError && <Alert>{errorMessage(q.error)}</Alert>}
      {del.isError && <Alert>{errorMessage(del.error)}</Alert>}
      {list.map((a) => (
        <div key={a.id} className="group flex items-center gap-1.5 rounded px-2 py-1 hover:bg-bg-hover">
          <Box size={13} className="shrink-0 text-fg-muted" />
          <div className="min-w-0 flex-1">
            <a href={artifactZipUrl(repo, a.id)} download={`${a.name}.zip`} className="block truncate text-[13px] hover:underline" title={`${a.name} (from ${a.job_name}, attempt ${a.attempt})`}>
              {a.name}
            </a>
            <div className="text-[11px] text-fg-faint">
              {formatBytes(a.size_bytes)} · {expiresText(a.expires_at)}
            </div>
          </div>
          <a href={artifactZipUrl(repo, a.id)} download={`${a.name}.zip`} aria-label={`Download ${a.name}`} className="rounded p-1 text-fg-muted hover:text-fg">
            <Download size={13} />
          </a>
          {canWrite && (
            <button
              type="button"
              aria-label={`Delete ${a.name}`}
              className="rounded p-1 text-fg-muted opacity-0 group-hover:opacity-100 hover:text-danger focus:opacity-100"
              disabled={del.isPending}
              onClick={() => window.confirm(`Delete artifact ${a.name}?`) && del.mutate(a.id)}
            >
              <Trash2 size={13} />
            </button>
          )}
        </div>
      ))}
    </section>
  )
}
