import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, CircleDot, Coffee, FolderGit2, GitPullRequest, Inbox, Play, Plus } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import type { IssueSummary, PullSummary, WorkflowRun } from '../../api/client'
import { allRunsQuery, myIssuesQuery, myPullsQuery, reposQuery, sessionQuery } from '../../api/queries'
import { StatusIcon } from '../../components/ci/common'
import { IssueStateIcon, LabelChip } from '../../components/issues/meta'
import { Page } from '../../components/Page'
import { DecisionBadge, PullStateIcon } from '../../components/pulls/common'
import { CreateRepoDialog } from '../../components/repos/CreateRepoDialog'
import { RepoChooser } from '../../components/repos/RepoChooser'
import { Avatar, Badge, Button, Card, EmptyState, ErrorState, Sep, SkeletonRows, StatCard, Time } from '../../components/ui'
import { durationText, isFailed } from '../../lib/ci'
import { formatDate, greeting } from '../../lib/format'

const LIMIT = 5

function ViewAll({ to, search }: { to: string; search?: Record<string, unknown> }) {
  return (
    <Link to={to} search={search} className="inline-flex items-center gap-1 text-sm text-fg-muted hover:text-fg">
      View all <ArrowRight size={12} />
    </Link>
  )
}

function Rows<T>({ q, empty, render }: { q: { isPending: boolean; isError: boolean; error: unknown; data?: T[] }; empty: ReactNode; render: (item: T) => ReactNode }) {
  if (q.isPending) return <SkeletonRows rows={3} />
  if (q.isError) return <ErrorState error={q.error} />
  if (!q.data?.length) return <div className="px-4 py-8 text-center text-sm text-fg-faint">{empty}</div>
  return <ul>{q.data.slice(0, LIMIT).map(render)}</ul>
}

const rowClass = 'relative flex items-start gap-3 border-b border-border px-4 py-2.5 transition-colors last:border-0 hover:bg-bg-hover/60'
const stretched = 'font-medium after:absolute after:inset-0 hover:text-accent'

function PullItem({ p }: { p: PullSummary }) {
  return (
    <li className={rowClass}>
      <span className="mt-0.5">
        <PullStateIcon pr={p} />
      </span>
      <div className="min-w-0 flex-1">
        <Link to="/$repo/pulls/$number" params={{ repo: p.repo, number: String(p.number) }} className={stretched}>
          {p.title}
        </Link>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-sm text-fg-muted">
          <span>
            {p.repo}#{p.number}
          </span>
          <Sep />
          <span className="inline-flex items-center gap-1">
            <Avatar user={p.author} size={14} /> {p.author.display_name}
          </span>
          <Sep />
          <Time iso={p.updated_at ?? p.created_at} />
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2 self-center">
        {p.draft && <Badge>draft</Badge>}
        <DecisionBadge decision={p.review_decision} />
      </div>
    </li>
  )
}

function IssueItem({ i }: { i: IssueSummary }) {
  return (
    <li className={rowClass}>
      <span className="mt-0.5">
        <IssueStateIcon issue={i} />
      </span>
      <div className="min-w-0 flex-1">
        <Link to="/$repo/issues/$number" params={{ repo: i.repo, number: String(i.number) }} className={stretched}>
          {i.title}
        </Link>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-sm text-fg-muted">
          <span>
            {i.repo}#{i.number}
          </span>
          <Sep />
          <Time iso={i.updated_at ?? i.created_at} />
          {i.labels.slice(0, 3).map((l) => (
            <LabelChip key={l.id} label={l} />
          ))}
        </div>
      </div>
    </li>
  )
}

function RunItem({ r }: { r: WorkflowRun }) {
  return (
    <li className={rowClass}>
      <StatusIcon status={r.status} conclusion={r.conclusion} className="mt-0.5" />
      <div className="min-w-0 flex-1">
        <Link to="/$repo/ci/runs/$number" params={{ repo: r.repo, number: String(r.number) }} search={{}} className={`block truncate ${stretched}`}>
          {r.title || r.workflow_name}
        </Link>
        <div className="mt-0.5 flex min-w-0 items-center gap-x-1.5 text-sm text-fg-muted">
          <span className="truncate">
            {r.repo} · {r.workflow_name}
          </span>
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-end text-sm text-fg-faint tabular-nums">
        <Time iso={r.created_at} />
        <span>{durationText(r.started_at, r.completed_at)}</span>
      </div>
    </li>
  )
}

export function HomePage() {
  const me = useQuery(sessionQuery).data?.user
  const isAdmin = me?.role === 'admin'
  const repos = useQuery(reposQuery)
  const reviews = useQuery(myPullsQuery('review_requested'))
  const authored = useQuery(myPullsQuery('authored'))
  const assigned = useQuery(myIssuesQuery('assigned'))
  const runs = useQuery({ ...allRunsQuery({ actor: '@me' }), refetchInterval: (q) => (q.state.data?.runs.some((r) => r.status !== 'completed') ? 5000 : 60_000) })
  const [creating, setCreating] = useState(false)
  const [choosing, setChoosing] = useState(false)
  const now = new Date()
  const firstName = me?.display_name.split(' ')[0] ?? ''
  const noRepos = repos.data?.length === 0

  const running = runs.data?.runs.filter((r) => r.status !== 'completed').length ?? 0
  const failed = runs.data?.runs.filter((r) => r.status === 'completed' && isFailed(r.conclusion)).length ?? 0
  const recentRepos = (repos.data ?? []).slice(0, 6)

  return (
    <Page
      title="Home"
      width="wide"
      actions={
        !noRepos && (
          <>
            <Button icon={<CircleDot size={14} />} onClick={() => setChoosing(true)}>
              New issue
            </Button>
            {isAdmin && (
              <Button variant="primary" icon={<Plus size={14} />} onClick={() => setCreating(true)}>
                New repository
              </Button>
            )}
          </>
        )
      }
    >
      <div className="mb-6">
        <p className="text-2xl font-semibold tracking-tight">
          {greeting(now.getHours())}
          {firstName && `, ${firstName}`}
        </p>
        <p className="mt-0.5 text-fg-muted">{formatDate(now.toISOString(), { weekday: 'long', month: 'long', day: 'numeric' })}</p>
      </div>

      {repos.isError ? (
        <ErrorState error={repos.error} className="p-0" />
      ) : noRepos ? (
        <Card>
          <EmptyState
            icon={<FolderGit2 size={20} />}
            title="No repositories yet"
            action={
              isAdmin && (
                <Button variant="primary" icon={<Plus size={14} />} onClick={() => setCreating(true)}>
                  Create the first repository
                </Button>
              )
            }
          >
            {isAdmin
              ? 'Create a repository to start pushing code, reviewing pull requests and running CI.'
              : "You don't have access to any repositories yet. Ask an admin to grant you access."}
          </EmptyState>
        </Card>
      ) : (
        <>
          <div className="mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatCard
              label="Review requests"
              value={reviews.data?.total ?? '–'}
              icon={<Inbox size={15} />}
              tone={reviews.data?.total ? 'accent' : undefined}
              hint={reviews.data?.total ? 'Waiting on you' : 'All caught up'}
              to="/pulls"
            />
            <StatCard label="Your open pull requests" value={authored.data?.total ?? '–'} icon={<GitPullRequest size={15} />} to="/pulls" search={{ filter: 'authored' }} />
            <StatCard label="Assigned issues" value={assigned.data?.total ?? '–'} icon={<CircleDot size={15} />} to="/issues" />
            <StatCard
              label="Your CI runs"
              value={runs.data?.total ?? '–'}
              icon={<Play size={15} />}
              tone={failed ? 'danger' : running ? 'accent' : undefined}
              hint={running ? `${running} running${failed ? ` · ${failed} failed` : ''}` : failed ? `${failed} failed recently` : 'Nothing running'}
              to="/ci"
              search={{ mine: true }}
            />
          </div>

          <div className="grid gap-6 lg:grid-cols-3">
            <div className="flex min-w-0 flex-col gap-6 lg:col-span-2">
              <Card title="Needs your review" flush actions={<ViewAll to="/pulls" />}>
                <Rows
                  q={{ ...reviews, data: reviews.data?.pulls }}
                  empty={
                    <span className="inline-flex items-center gap-1.5">
                      <Coffee size={14} /> You're all caught up.
                    </span>
                  }
                  render={(p) => <PullItem key={`${p.repo}#${p.number}`} p={p} />}
                />
              </Card>
              <Card title="Your pull requests" flush actions={<ViewAll to="/pulls" search={{ filter: 'authored' }} />}>
                <Rows q={{ ...authored, data: authored.data?.pulls }} empty="You have no open pull requests." render={(p) => <PullItem key={`${p.repo}#${p.number}`} p={p} />} />
              </Card>
              <Card title="Assigned to you" flush actions={<ViewAll to="/issues" />}>
                <Rows q={{ ...assigned, data: assigned.data?.issues }} empty="Nothing is assigned to you." render={(i) => <IssueItem key={`${i.repo}#${i.number}`} i={i} />} />
              </Card>
            </div>
            <div className="flex min-w-0 flex-col gap-6">
              <Card title="Recent repositories" flush actions={<ViewAll to="/repos" />}>
                {repos.isPending ? (
                  <SkeletonRows rows={4} />
                ) : (
                  <ul>
                    {recentRepos.map((r) => (
                      <li key={r.id} className={rowClass}>
                        <span
                          className="mt-px flex size-5 shrink-0 items-center justify-center rounded text-xs font-semibold text-white uppercase"
                          style={{ backgroundColor: `var(--avatar-${(r.id % 8) + 1})` }}
                          aria-hidden
                        >
                          {r.name.slice(0, 1)}
                        </span>
                        <div className="min-w-0 flex-1">
                          <Link to="/$repo" params={{ repo: r.name }} className={`block truncate ${stretched}`}>
                            {r.name}
                          </Link>
                          <div className="truncate text-sm text-fg-faint">{r.pushed_at ? <>Pushed <Time iso={r.pushed_at} /></> : 'No pushes yet'}</div>
                        </div>
                      </li>
                    ))}
                  </ul>
                )}
              </Card>
              <Card title="Your recent runs" flush actions={<ViewAll to="/ci" search={{ mine: true }} />}>
                <Rows q={{ ...runs, data: runs.data?.runs }} empty="You haven't triggered any runs." render={(r) => <RunItem key={r.id} r={r} />} />
              </Card>
            </div>
          </div>
        </>
      )}
      {isAdmin && <CreateRepoDialog open={creating} onClose={() => setCreating(false)} />}
      <RepoChooser intent={choosing ? 'issue' : null} onClose={() => setChoosing(false)} />
    </Page>
  )
}
