import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowUpDown, CheckCircle2, CircleDot, Milestone as MilestoneIcon, Plus, SearchX, Tag, User, UserPen, X } from 'lucide-react'
import { issuesQuery, milestonesQuery, repoQuery, usersQuery, type IssueFilters } from '../../api/queries'
import { LabelPicker } from '../../components/issues/meta'
import { EmptyState } from '../../components/Page'
import { Avatar, Button, ButtonLink, ErrorState, FilterBar, FilterMenu, FilterPicker, IconLink, Pagination, SearchInput, SegmentedControl, SkeletonRows, type SelectItem } from '../../components/ui'
import { hasFilters, splitLabels, toggleLabel, type IssueListSearch } from '../../lib/issues'
import { canTriage } from '../../lib/permissions'
import { useRepoName } from '../repos/RepoLayout'
import { IssueRows } from './IssueList'

export { canTriage }

const PER_PAGE = 50

export function RepoIssuesPage() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const search = useSearch({ strict: false }) as IssueListSearch & { page?: number }
  const navigate = useNavigate()
  const set = (s: Partial<IssueListSearch & { page?: number }>, replace = false) =>
    void navigate({ to: '/$repo/issues', params: { repo: name }, search: { ...search, page: undefined, ...s }, replace })

  const state = search.state ?? 'open'
  const page = search.page ?? 1
  const filters: IssueFilters = { labels: search.labels, milestone: search.milestone, assignee: search.assignee, author: search.author, q: search.q }
  const issues = useQuery({ ...issuesQuery(name, { ...filters, state, sort: search.sort, page }), placeholderData: (prev) => prev })
  const openCount = useQuery({ ...issuesQuery(name, { ...filters, state: 'open', per_page: 1 }), placeholderData: (prev) => prev })
  const closedCount = useQuery({ ...issuesQuery(name, { ...filters, state: 'closed', per_page: 1 }), placeholderData: (prev) => prev })
  const milestones = useQuery(milestonesQuery(name, 'all'))
  const users = useQuery(usersQuery)
  const labels = splitLabels(search.labels)
  const filtered = hasFilters(search)
  const canNew = canTriage(repo.permission) && !repo.archived

  const people = (users.data ?? []).map<SelectItem>((u) => ({
    key: u.username,
    label: u.display_name,
    search: u.username,
    render: (
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <Avatar user={u} size={18} />
        <span className="truncate">{u.display_name}</span>
      </span>
    ),
  }))

  return (
    <div className="flex flex-col">
      <FilterBar
        className="px-5"
        end={
          <>
            <IconLink to="/$repo/labels" params={{ repo: name }} label="Labels" size="sm">
              <Tag size={13} />
            </IconLink>
            <IconLink to="/$repo/milestones" params={{ repo: name }} label="Milestones" size="sm">
              <MilestoneIcon size={13} />
            </IconLink>
            {canNew && (
              <ButtonLink to="/$repo/issues/new" params={{ repo: name }} variant="primary" icon={<Plus size={14} />}>
                New issue
              </ButtonLink>
            )}
          </>
        }
      >
        <SegmentedControl
          label="State"
          value={state === 'closed' ? 'closed' : 'open'}
          onChange={(s) => set({ state: s === 'open' ? undefined : s })}
          options={[
            { value: 'open', label: 'Open', icon: <CircleDot size={12} />, count: openCount.data?.total },
            { value: 'closed', label: 'Closed', icon: <CheckCircle2 size={12} />, count: closedCount.data?.total },
          ]}
        />
        <SearchInput value={search.q ?? ''} onChange={(q) => set({ q: q || undefined }, true)} debounce={250} placeholder="Search titles…" label="Search issues" shortcut className="max-w-56" />
        <LabelPicker
          repo={name}
          value={labels}
          onChange={(l) => set({ labels: l.length ? l.join(',') : undefined })}
          trigger={
            <>
              <Tag size={12} /> Labels{labels.length > 0 && ` (${labels.length})`}
            </>
          }
        />
        <FilterPicker
          title="Milestone"
          icon={<MilestoneIcon size={12} />}
          value={search.milestone}
          onChange={(v) => set({ milestone: v })}
          items={[
            { key: '*', label: 'Has a milestone' },
            { key: 'none', label: 'No milestone' },
            ...(milestones.data ?? []).map((m) => ({ key: m.title, label: m.state === 'closed' ? `${m.title} (closed)` : m.title, group: 'Milestones' })),
          ]}
        />
        <FilterPicker title="Assignee" icon={<User size={12} />} value={search.assignee} onChange={(v) => set({ assignee: v })} items={[{ key: 'none', label: 'Unassigned' }, ...people]} />
        <FilterPicker title="Author" icon={<UserPen size={12} />} value={search.author} onChange={(v) => set({ author: v })} items={people} />
        <FilterMenu
          label="Sort"
          icon={<ArrowUpDown size={12} />}
          value={search.sort ?? 'updated'}
          onChange={(v) => set({ sort: v === 'updated' ? undefined : v })}
          options={[
            { value: 'updated', label: 'Recently updated' },
            { value: 'created', label: 'Newest' },
            { value: 'comments', label: 'Most commented' },
          ]}
        />
        {filtered && (
          <Button variant="ghost" size="sm" icon={<X size={12} />} onClick={() => void navigate({ to: '/$repo/issues', params: { repo: name }, search: { state: search.state } })}>
            Clear filters
          </Button>
        )}
      </FilterBar>
      {issues.isPending ? (
        <SkeletonRows rows={6} />
      ) : issues.isError ? (
        <ErrorState error={issues.error} />
      ) : issues.data.issues.length === 0 ? (
        filtered ? (
          <EmptyState icon={<SearchX size={20} />} title="No matching issues">
            Try different filters.
          </EmptyState>
        ) : (
          <EmptyState
            icon={state === 'closed' ? <CheckCircle2 size={20} /> : <CircleDot size={20} />}
            title={`No ${state} issues`}
            action={
              state === 'open' &&
              canNew && (
                <ButtonLink to="/$repo/issues/new" params={{ repo: name }} variant="secondary" icon={<Plus size={14} />}>
                  Report an issue
                </ButtonLink>
              )
            }
          >
            {state === 'open' && canNew && 'Report a bug or suggest an idea.'}
          </EmptyState>
        )
      ) : (
        <>
          <IssueRows issues={issues.data.issues} onLabel={(l) => set({ labels: toggleLabel(search.labels, l) })} />
          <Pagination page={page} total={issues.data.total} perPage={PER_PAGE} onPage={(p) => set({ page: p > 1 ? p : undefined })} />
        </>
      )}
    </div>
  )
}
