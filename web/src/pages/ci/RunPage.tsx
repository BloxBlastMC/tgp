import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import {
  ArrowDownToLine,
  Ban,
  Box,
  ChevronDown,
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  ChevronUp,
  Clock,
  Cpu,
  Download,
  GitCommit,
  HardDrive,
  MoreHorizontal,
  Play,
  RotateCw,
  Search,
  Server,
  Timer,
  Trash2,
  WrapText,
} from 'lucide-react'
import { createContext, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { api, ApiRequestError, errorMessage, unwrap, type Job, type WorkflowRunDetail } from '../../api/client'
import { repoQuery, runArtifactsQuery, runQuery } from '../../api/queries'
import { Duration, EventBadge, eventLabel, KindIcon, StatusIcon } from '../../components/ci/common'
import { LogViewContext, StepLog, useJobLogs, type LogView } from '../../components/ci/logs'
import { EmptyState } from '../../components/Page'
import { Alert, Badge, Button, cx, Dot, Eyebrow, IconButton, Input, Menu, MenuItem, MenuSeparator, PageSkeleton, Time, Tooltip, useConfirm, useToast } from '../../components/ui'
import { canWrite as canWritePerm } from '../../lib/permissions'
import { defaultJob, findMatches, formatBytes, groupJobs, groupKind, isFailed, linesByStep, logText, statusKind, statusLabel, statusTone, stepMatch, stepTitle } from '../../lib/ci'
import { useRepoName } from '../repos/RepoLayout'
import { artifactZipUrl, expiresText } from '../../lib/releases'

export function RunPage() {
  const repo = useRepoName()
  const { number } = useParams({ strict: false }) as { number: string }
  const n = Number(number)
  const search = useSearch({ strict: false }) as { job?: number }
  const run = useQuery({ ...runQuery(repo, n), refetchInterval: (q) => (q.state.data && q.state.data.status !== 'completed' ? 3000 : false) })

  if (run.isPending) return <PageSkeleton rows={4} />
  if (run.isError) {
    if (run.error instanceof ApiRequestError && run.error.status === 404)
      return (
        <EmptyState icon={<Play size={20} />} title="Run not found">
          There's no run #{number} in {repo}.
        </EmptyState>
      )
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
        <div className="flex w-64 shrink-0 flex-col overflow-auto border-r border-border bg-bg-sidebar/40">
          <JobList repo={repo} run={r} selected={job?.id} />
          <ArtifactList repo={repo} run={r} />
        </div>
        <div className="min-w-0 flex-1">
          {job ? (
            <JobPanel key={job.id} repo={repo} run={r} job={job} />
          ) : (
            <EmptyState icon={<Play size={20} />} title="No jobs">
              This run has no jobs.
            </EmptyState>
          )}
        </div>
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
  const confirm = useConfirm()
  const kind = statusKind(run.status, run.conclusion)
  const anyFailed = run.jobs.some((j) => j.status === 'completed' && isFailed(j.conclusion))
  const err = rerun.error ?? cancel.error
  return (
    <div className="shrink-0 border-b border-border px-6 py-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-xl font-semibold tracking-tight">{run.title || run.workflow_name}</h1>
            <span className="text-fg-faint">#{run.number}</span>
            <Badge tone={statusTone[kind]}>{statusLabel[kind]}</Badge>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-fg-muted">
            <span>
              workflow{' '}
              <Link to="/$repo/ci" params={{ repo }} search={{ workflow: run.workflow_path.split('/').pop() }} className="font-medium text-fg hover:underline" title={run.workflow_path}>
                {run.workflow_name}
              </Link>
            </span>
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
            <Button
              variant="danger"
              icon={<Ban size={13} />}
              loading={cancel.isPending}
              onClick={async () => {
                const ok = await confirm({
                  title: `Cancel run #${run.number}?`,
                  description: 'Running jobs are stopped and queued jobs never start.',
                  confirmLabel: 'Cancel run',
                  cancelLabel: 'Keep running',
                })
                if (ok) cancel.mutate()
              }}
            >
              Cancel run
            </Button>
          )}
          {run.can_rerun && anyFailed && (
            <Button icon={<RotateCw size={13} />} loading={rerun.isPending && rerun.variables === true} onClick={() => rerun.mutate(true)}>
              Re-run failed jobs
            </Button>
          )}
          {run.can_rerun && (
            <Button variant={anyFailed ? 'secondary' : 'primary'} icon={<RotateCw size={13} />} loading={rerun.isPending && rerun.variables === false} onClick={() => rerun.mutate(false)}>
              Re-run all jobs
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
      className={cx('flex h-8 items-center gap-2 rounded-md pr-2 text-base transition-colors', nested ? 'pl-6' : 'pl-2', selected ? 'bg-bg-active font-medium text-fg' : 'text-fg-muted hover:bg-bg-hover hover:text-fg')}
      aria-current={selected ? 'page' : undefined}
    >
      <StatusIcon status={job.status} conclusion={job.conclusion} size={14} />
      <span className="min-w-0 flex-1 truncate">{nested && job.matrix ? matrixText(job) : job.name}</span>
      <span className="shrink-0 text-xs text-fg-faint">
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
      <Eyebrow as="h2" className="px-2 pt-1 pb-2">
        Jobs
      </Eyebrow>
      {groups.map((g) =>
        g.jobs.length === 1 && !g.jobs[0]!.matrix ? (
          <JobRow key={g.key} repo={repo} run={run} job={g.jobs[0]!} selected={g.jobs[0]!.id === selected} />
        ) : (
          <div key={g.key}>
            <button type="button" onClick={() => setClosed((c) => ({ ...c, [g.key]: !c[g.key] }))} className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-base text-fg-muted hover:bg-bg-hover hover:text-fg" aria-expanded={!closed[g.key]}>
              <KindIcon kind={groupKind(g.jobs)} size={14} />
              <span className="min-w-0 flex-1 truncate font-medium">{g.key}</span>
              <span className="text-xs text-fg-faint">{g.jobs.length}</span>
              {closed[g.key] ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
            </button>
            {!closed[g.key] && g.jobs.map((j) => <JobRow key={j.id} repo={repo} run={run} job={j} selected={j.id === selected} nested />)}
          </div>
        ),
      )}
    </nav>
  )
}

function usePref(key: string, initial: boolean): [boolean, (v: boolean) => void] {
  const [v, setV] = useState(() => {
    try {
      const s = localStorage.getItem(key)
      return s == null ? initial : s === '1'
    } catch {
      return initial
    }
  })
  return [
    v,
    (next: boolean) => {
      setV(next)
      try {
        localStorage.setItem(key, next ? '1' : '0')
      } catch {
        /* ignore */
      }
    },
  ]
}

/** Bumped by "expand all" / "collapse all"; steps follow the latest value. */
const ExpandContext = createContext<{ open: boolean; n: number } | null>(null)

function JobPanel({ repo, run, job }: { repo: string; run: WorkflowRunDetail; job: Job }) {
  const qc = useQueryClient()
  const toast = useToast()
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

  const [wrap, setWrap] = usePref('tgp-log-wrap', true)
  const [timestamps, setTimestamps] = usePref('tgp-log-time', false)
  const [follow, setFollow] = useState(true)
  const [query, setQuery] = useState('')
  const [matchIdx, setMatchIdx] = useState(0)
  const [expand, setExpand] = useState<{ open: boolean; n: number } | null>(null)
  const matches = useMemo(() => findMatches(logs.lines, query), [logs.lines, query])
  const active = matches.length ? (matches[Math.min(matchIdx, matches.length - 1)] ?? null) : null
  const maxN = logs.lines.length ? logs.lines[logs.lines.length - 1]!.n : 0
  const view: LogView = useMemo(
    () => ({ wrap, timestamps, gutter: Math.max(3, String(maxN).length), matches: new Set(matches), active }),
    [wrap, timestamps, maxN, matches, active],
  )

  // Auto-scroll while following a running job, unless the user scrolled up.
  const pane = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el = pane.current
    if (el && follow && !query && job.status === 'in_progress') el.scrollTop = el.scrollHeight
  }, [logs.lines.length, job.status, follow, query])
  // Bring the active match into view.
  useEffect(() => {
    if (active == null) return
    const raf = requestAnimationFrame(() => pane.current?.querySelector(`[data-line="${active}"]`)?.scrollIntoView({ block: 'center' }))
    return () => cancelAnimationFrame(raf)
  }, [active])
  // Jump to #L<n> once lines have loaded.
  const jumped = useRef(false)
  useEffect(() => {
    if (jumped.current || !logs.lines.length) return
    const m = window.location.hash.match(/^#L(\d+)$/)
    if (!m) return
    jumped.current = true
    window.setTimeout(() => document.getElementById(`L${m[1]}`)?.scrollIntoView({ block: 'center' }), 50)
  }, [logs.lines.length])

  const download = () => {
    const url = URL.createObjectURL(new Blob([logText(logs.lines)], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `${repo}-run${run.number}-${job.name.replace(/[^\w.-]+/g, '_')}.log`
    a.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return (
    <div
      ref={pane}
      className="h-full overflow-auto"
      onScroll={(e) => {
        const el = e.currentTarget
        if (job.status !== 'in_progress') return
        const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
        if (atBottom !== follow) setFollow(atBottom)
      }}
    >
      <div className="sticky top-0 z-10 border-b border-border bg-bg/95 backdrop-blur">
        <div className="px-5 pt-3 pb-2.5">
          <div className="flex items-center gap-2">
            <KindIcon kind={kind} size={16} />
            <h2 className="min-w-0 flex-1 truncate font-semibold">{job.name}</h2>
            <span className="text-sm text-fg-faint">
              <Duration start={job.started_at} end={job.completed_at} />
            </span>
            {logs.live && job.status === 'in_progress' && (
              <Badge tone="accent" icon={<Dot tone="accent" pulse />}>
                live
              </Badge>
            )}
            {run.can_rerun && job.status === 'completed' && (
              <Button variant="ghost" size="sm" icon={<RotateCw size={13} />} loading={rerunJob.isPending} onClick={() => rerunJob.mutate(job.id)} aria-label={`Re-run ${job.name}`}>
                Re-run job
              </Button>
            )}
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-fg-faint">
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
            <span title="Process limit">{job.resources.pids} processes</span>
            <span className="inline-flex items-center gap-1" title="Timeout">
              <Timer size={11} /> {job.timeout_minutes}m timeout
            </span>
            {job.labels.length > 0 && <span title="Runner labels (runs-on)">runs-on: {job.labels.join(', ')}</span>}
            {job.attempt > 1 && <span>attempt {job.attempt}</span>}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-1.5 border-t border-border bg-bg-subtle px-5 py-1.5">
          <div className="relative w-64 max-w-full">
            <Input
              size="sm"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value)
                setMatchIdx(0)
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  setMatchIdx((i) => stepMatch(i, e.shiftKey ? -1 : 1, matches.length))
                } else if (e.key === 'Escape') setQuery('')
              }}
              placeholder="Search logs"
              aria-label="Search logs"
              leading={<Search size={12} />}
              className="pr-16"
            />
            {query && (
              <span className="absolute top-1/2 right-2 -translate-y-1/2 text-xs text-fg-faint tabular-nums" aria-live="polite">
                {matches.length ? `${Math.min(matchIdx, matches.length - 1) + 1} of ${matches.length}` : 'No matches'}
              </span>
            )}
          </div>
          <IconButton label="Previous match" size="sm" disabled={!matches.length} onClick={() => setMatchIdx((i) => stepMatch(i, -1, matches.length))}>
            <ChevronUp size={13} />
          </IconButton>
          <IconButton label="Next match" size="sm" disabled={!matches.length} onClick={() => setMatchIdx((i) => stepMatch(i, 1, matches.length))}>
            <ChevronDown size={13} />
          </IconButton>
          <span className="mx-1 hidden h-4 w-px bg-border xl:block" aria-hidden />
          <div className="hidden items-center gap-1.5 xl:flex">
          <IconButton label="Wrap long lines" size="sm" pressed={wrap} onClick={() => setWrap(!wrap)}>
            <WrapText size={13} />
          </IconButton>
          <IconButton label="Show timestamps" size="sm" pressed={timestamps} onClick={() => setTimestamps(!timestamps)}>
            <Clock size={13} />
          </IconButton>
          <IconButton label="Expand all steps" size="sm" onClick={() => setExpand((x) => ({ open: true, n: (x?.n ?? 0) + 1 }))}>
            <ChevronsUpDown size={13} />
          </IconButton>
          <IconButton label="Collapse all steps" size="sm" onClick={() => setExpand((x) => ({ open: false, n: (x?.n ?? 0) + 1 }))}>
            <ChevronsDownUp size={13} />
          </IconButton>
          </div>
          <span className="flex-1" />
          {job.status === 'in_progress' && (
            <Button
              size="sm"
              variant={follow ? 'secondary' : 'ghost'}
              icon={<ArrowDownToLine size={12} />}
              aria-pressed={follow}
              onClick={() => {
                setFollow(true)
                pane.current?.scrollTo({ top: pane.current.scrollHeight })
              }}
            >
              {follow ? 'Following' : 'Follow log'}
            </Button>
          )}
          <Button
            size="sm"
            variant="ghost"
            icon={<Download size={12} />}
            disabled={!logs.lines.length}
            className="hidden xl:inline-flex"
            onClick={() => {
              download()
              toast.success('Log downloaded')
            }}
          >
            Download log
          </Button>
          <div className="xl:hidden">
            <Menu
              placement="bottom-end"
              label="Log options"
              trigger={(t) => (
                <IconButton {...t} label="Log options" size="sm">
                  <MoreHorizontal size={13} />
                </IconButton>
              )}
            >
              <MenuItem icon={<WrapText size={13} />} onSelect={() => setWrap(!wrap)}>
                {wrap ? "Don't wrap long lines" : 'Wrap long lines'}
              </MenuItem>
              <MenuItem icon={<Clock size={13} />} onSelect={() => setTimestamps(!timestamps)}>
                {timestamps ? 'Hide timestamps' : 'Show timestamps'}
              </MenuItem>
              <MenuSeparator />
              <MenuItem icon={<ChevronsUpDown size={13} />} onSelect={() => setExpand((x) => ({ open: true, n: (x?.n ?? 0) + 1 }))}>
                Expand all steps
              </MenuItem>
              <MenuItem icon={<ChevronsDownUp size={13} />} onSelect={() => setExpand((x) => ({ open: false, n: (x?.n ?? 0) + 1 }))}>
                Collapse all steps
              </MenuItem>
              <MenuSeparator />
              <MenuItem
                icon={<Download size={13} />}
                disabled={!logs.lines.length}
                onSelect={() => {
                  download()
                  toast.success('Log downloaded')
                }}
              >
                Download log
              </MenuItem>
            </Menu>
          </div>
        </div>
        {rerunJob.isError && (
          <div className="px-5 pb-2">
            <Alert>{errorMessage(rerunJob.error)}</Alert>
          </div>
        )}
      </div>
      <LogViewContext.Provider value={view}>
        <ExpandContext.Provider value={expand}>
          <div className="flex flex-col gap-2 p-3">
            {job.failure_reason && <Alert>{job.failure_reason}</Alert>}
            {job.status === 'queued' && <Alert tone="warning">{job.wait_reason ? (/^waiting/i.test(job.wait_reason) ? job.wait_reason : `Waiting: ${job.wait_reason}`) : 'Waiting for a runner to pick up this job…'}</Alert>}
            {job.status === 'pending' && <Alert tone="info">{job.wait_reason ?? (job.needs.length ? `Waiting for ${job.needs.join(', ')} to finish.` : 'Waiting to start.')}</Alert>}
            {logs.error && !logs.live && <Alert tone="warning">Couldn't load logs: {logs.error}</Alert>}
            {logs.truncated && <Alert tone="warning">The log reached the size limit; later output was dropped.</Alert>}
            <ul className="flex flex-col overflow-hidden rounded-lg border border-border bg-surface-1" aria-label="Steps">
              {steps.map((s) => (
                <StepSection key={s.number} step={s} lines={byStep.get(s.number) ?? []} jobRunning={job.status === 'in_progress'} />
              ))}
            </ul>
            {steps.length === 0 && job.status === 'completed' && <p className="px-2 text-fg-faint">This job produced no output.</p>}
          </div>
        </ExpandContext.Provider>
      </LogViewContext.Provider>
    </div>
  )
}

type StepView = { number: number; name: string; status: string; conclusion: string | null; started_at?: string | null; completed_at?: string | null }

function StepSection({ step, lines, jobRunning }: { step: StepView; lines: { n: number; step: number; t: string; text: string }[]; jobRunning: boolean }) {
  const auto = step.status === 'in_progress' || step.conclusion === 'failure' || (jobRunning && step.number === 0 && step.status === 'in_progress')
  const [open, setOpen] = useState<boolean | null>(null)
  const expand = useContext(ExpandContext)
  const view = useContext(LogViewContext)
  useEffect(() => {
    if (expand) setOpen(expand.open)
  }, [expand])
  const hasMatch = view.matches.size > 0 && lines.some((l) => view.matches.has(l.n))
  const isOpen = hasMatch || (open ?? auto)
  return (
    <li className="border-b border-border last:border-0" data-testid="step">
      <button type="button" onClick={() => setOpen(!isOpen)} className="flex h-9 w-full items-center gap-2 px-3 text-left transition-colors hover:bg-bg-hover" aria-expanded={isOpen}>
        {isOpen ? <ChevronDown size={13} className="text-fg-faint" /> : <ChevronRight size={13} className="text-fg-faint" />}
        <StatusIcon status={step.status} conclusion={step.conclusion ?? (step.number <= 0 && step.status === 'completed' ? 'success' : null)} size={14} />
        <span className={cx('min-w-0 flex-1 truncate', isOpen && 'font-medium')}>{step.name}</span>
        {hasMatch && <Badge tone="warning">match</Badge>}
        <span className="text-xs text-fg-faint">
          <Duration start={step.started_at} end={step.completed_at} />
        </span>
      </button>
      {isOpen && (
        <div className="border-t border-border bg-bg py-1.5">
          {lines.length ? <StepLog lines={lines} /> : <p className="px-4 py-1 font-mono text-sm text-fg-faint">{step.status === 'pending' ? 'Not started.' : step.conclusion === 'skipped' ? 'Skipped.' : 'No output.'}</p>}
        </div>
      )}
    </li>
  )
}

function ArtifactList({ repo, run }: { repo: string; run: WorkflowRunDetail }) {
  const qc = useQueryClient()
  const perm = useQuery(repoQuery(repo)).data?.permission
  const canWrite = canWritePerm(perm)
  const confirm = useConfirm()
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
      <Eyebrow as="h2" className="px-2 pt-1 pb-2">
        Artifacts <span className="font-normal">{list.length}</span>
      </Eyebrow>
      {q.isError && <Alert>{errorMessage(q.error)}</Alert>}
      {del.isError && <Alert>{errorMessage(del.error)}</Alert>}
      {list.map((a) => (
        <div key={a.id} className="group flex items-center gap-1.5 rounded-md px-2 py-1 hover:bg-bg-hover">
          <Box size={13} className="shrink-0 text-fg-muted" />
          <div className="min-w-0 flex-1">
            <a href={artifactZipUrl(repo, a.id)} download={`${a.name}.zip`} className="block truncate text-base hover:underline" title={`${a.name} (from ${a.job_name}, attempt ${a.attempt})`}>
              {a.name}
            </a>
            <div className="text-xs text-fg-faint">
              {formatBytes(a.size_bytes)} · {expiresText(a.expires_at)}
            </div>
          </div>
          <Tooltip content={`Download ${a.name}`} describe={false}>
            <a href={artifactZipUrl(repo, a.id)} download={`${a.name}.zip`} aria-label={`Download ${a.name}`} className="rounded-md p-1 text-fg-muted hover:bg-bg-active hover:text-fg">
              <Download size={13} />
            </a>
          </Tooltip>
          {canWrite && (
            <IconButton
              label={`Delete ${a.name}`}
              size="xs"
              className="text-fg-faint hover:bg-danger/10 hover:text-danger"
              loading={del.isPending && del.variables === a.id}
              onClick={async () => {
                if (await confirm({ title: `Delete artifact ${a.name}?`, description: "Downloads stop working. This can't be undone.", confirmLabel: 'Delete artifact' })) del.mutate(a.id)
              }}
            >
              <Trash2 size={12} />
            </IconButton>
          )}
        </div>
      ))}
    </section>
  )
}
