import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { CheckCircle2, ChevronDown, CircleDot, Milestone as MilestoneIcon, Plus, Search, Tag, X } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { errorMessage } from '../../api/client'
import { issuesQuery, milestonesQuery, repoQuery, usersQuery, type IssueFilters } from '../../api/queries'
import { LabelPicker } from '../../components/issues/meta'
import { EmptyState } from '../../components/Page'
import { Alert, Button, cx, Input, Select, Spinner } from '../../components/ui'
import { hasFilters, splitLabels, toggleLabel, type IssueListSearch } from '../../lib/issues'
import { useRepoName } from '../repos/RepoLayout'
import { IssueRows } from './IssueList'

export function canTriage(permission: string): boolean {
  return permission === 'triage' || permission === 'write' || permission === 'admin'
}

/** A compact filter dropdown (Select is full-width by default). */
function FilterSelect({ label, value, onChange, width, children }: { label: string; value: string; onChange: (v: string) => void; width: string; children: ReactNode }) {
  return (
    <div className={cx('shrink-0', width)}>
      <Select value={value} onChange={(e) => onChange(e.target.value)} aria-label={label} className="h-7 text-[12px]">
        {children}
      </Select>
    </div>
  )
}

export function RepoIssuesPage() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const search = useSearch({ strict: false }) as IssueListSearch
  const navigate = useNavigate()
  const set = (s: Partial<IssueListSearch>) => void navigate({ to: '/$repo/issues', params: { repo: name }, search: { ...search, ...s } })
  const [q, setQ] = useState(search.q ?? '')
  useEffect(() => {
    const t = window.setTimeout(() => {
      if ((search.q ?? '') !== q) void navigate({ to: '/$repo/issues', params: { repo: name }, search: { ...search, q: q || undefined }, replace: true })
    }, 250)
    return () => window.clearTimeout(t)
  }, [q, search, name, navigate])

  const state = search.state ?? 'open'
  const filters: IssueFilters = { labels: search.labels, milestone: search.milestone, assignee: search.assignee, author: search.author, q: search.q }
  const issues = useQuery(issuesQuery(name, { ...filters, state, sort: search.sort }))
  const openCount = useQuery(issuesQuery(name, { ...filters, state: 'open', per_page: 1 }))
  const closedCount = useQuery(issuesQuery(name, { ...filters, state: 'closed', per_page: 1 }))
  const milestones = useQuery(milestonesQuery(name, 'all'))
  const users = useQuery(usersQuery)
  const labels = splitLabels(search.labels)
  const counts = { open: openCount.data?.total, closed: closedCount.data?.total }

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <div className="flex rounded border border-border-strong" role="group" aria-label="State">
          {(['open', 'closed'] as const).map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => set({ state: s === 'open' ? undefined : s })}
              aria-pressed={state === s}
              className={cx('flex h-7 items-center gap-1.5 px-2.5 text-[12px]', state === s ? 'bg-bg-hover text-fg' : 'text-fg-muted hover:text-fg')}
            >
              {s === 'open' ? <CircleDot size={13} /> : <CheckCircle2 size={13} />}
              {counts[s] ?? ''} {s === 'open' ? 'Open' : 'Closed'}
            </button>
          ))}
        </div>
        <div className="relative w-56">
          <Search size={13} className="absolute top-1/2 left-2 -translate-y-1/2 text-fg-faint" />
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search titles" className="h-7 pl-7" aria-label="Search issues" />
        </div>
        <LabelPicker
          repo={name}
          value={labels}
          onChange={(l) => set({ labels: l.length ? l.join(',') : undefined })}
          trigger={
            <>
              <Tag size={12} /> Labels{labels.length > 0 && ` (${labels.length})`} <ChevronDown size={12} />
            </>
          }
        />
        <FilterSelect label="Milestone" width="w-36" value={search.milestone ?? ''} onChange={(v) => set({ milestone: v || undefined })}>
          <option value="">Any milestone</option>
          <option value="*">Has a milestone</option>
          <option value="none">No milestone</option>
          {(milestones.data ?? []).map((m) => (
            <option key={m.id} value={m.title}>
              {m.title}
              {m.state === 'closed' ? ' (closed)' : ''}
            </option>
          ))}
        </FilterSelect>
        <FilterSelect label="Assignee" width="w-36" value={search.assignee ?? ''} onChange={(v) => set({ assignee: v || undefined })}>
          <option value="">Any assignee</option>
          <option value="none">Unassigned</option>
          {(users.data ?? []).map((u) => (
            <option key={u.id} value={u.username}>
              {u.display_name}
            </option>
          ))}
        </FilterSelect>
        <FilterSelect label="Author" width="w-32" value={search.author ?? ''} onChange={(v) => set({ author: v || undefined })}>
          <option value="">Any author</option>
          {(users.data ?? []).map((u) => (
            <option key={u.id} value={u.username}>
              {u.display_name}
            </option>
          ))}
        </FilterSelect>
        <FilterSelect label="Sort" width="w-28" value={search.sort ?? 'updated'} onChange={(v) => set({ sort: v === 'updated' ? undefined : (v as IssueListSearch['sort']) })}>
          <option value="updated">Updated</option>
          <option value="created">Newest</option>
          <option value="comments">Comments</option>
        </FilterSelect>
        {hasFilters(search) && (
          <button
            type="button"
            className="flex items-center gap-1 text-[12px] text-fg-muted hover:text-fg"
            onClick={() => {
              setQ('')
              void navigate({ to: '/$repo/issues', params: { repo: name }, search: { state: search.state } })
            }}
          >
            <X size={12} /> Clear filters
          </button>
        )}
        <div className="flex-1" />
        <Link to="/$repo/labels" params={{ repo: name }} className="flex items-center gap-1 text-[12px] text-fg-muted hover:text-fg">
          <Tag size={12} /> Labels
        </Link>
        <Link to="/$repo/milestones" params={{ repo: name }} className="flex items-center gap-1 text-[12px] text-fg-muted hover:text-fg">
          <MilestoneIcon size={12} /> Milestones
        </Link>
        {canTriage(repo.permission) && (
          <Link to="/$repo/issues/new" params={{ repo: name }}>
            <Button variant="primary">
              <Plus size={14} /> New issue
            </Button>
          </Link>
        )}
      </div>
      {issues.isPending ? (
        <Spinner />
      ) : issues.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(issues.error)}</Alert>
        </div>
      ) : issues.data.issues.length === 0 ? (
        <EmptyState title={hasFilters(search) ? 'No matching issues' : `No ${state} issues`}>
          {state === 'open' && !hasFilters(search) && canTriage(repo.permission) && 'Report a bug or suggest an idea with “New issue”.'}
        </EmptyState>
      ) : (
        <IssueRows issues={issues.data.issues} onLabel={(l) => set({ labels: toggleLabel(search.labels, l) })} />
      )}
    </div>
  )
}
