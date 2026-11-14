import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CircleDot, Inbox, SearchX } from 'lucide-react'
import { useMemo, useState } from 'react'
import { myIssuesQuery, type MyIssuesFilter } from '../../api/queries'
import { EmptyState, Page } from '../../components/Page'
import { ErrorState, FilterBar, SearchInput, SegmentedControl, SkeletonRows } from '../../components/ui'
import { IssueRows } from './IssueList'

type State = 'open' | 'closed' | 'all'
export type GlobalIssuesSearch = { filter?: MyIssuesFilter; state?: State }

export function validateGlobalIssues(s: Record<string, unknown>): GlobalIssuesSearch {
  const out: GlobalIssuesSearch = {}
  if (s.filter === 'created' || s.filter === 'all') out.filter = s.filter
  if (s.state === 'closed' || s.state === 'all') out.state = s.state
  return out
}

const filters: { value: MyIssuesFilter; label: string }[] = [
  { value: 'assigned', label: 'Assigned to me' },
  { value: 'created', label: 'Created by me' },
  { value: 'all', label: 'All' },
]

const states: { value: State; label: string }[] = [
  { value: 'open', label: 'Open' },
  { value: 'closed', label: 'Closed' },
  { value: 'all', label: 'All' },
]

const emptyText: Record<MyIssuesFilter, string> = {
  assigned: 'Nothing is assigned to you.',
  created: "You haven't opened any issues.",
  all: 'No issues in the repositories you can access.',
}

export function GlobalIssuesPage() {
  const search = useSearch({ strict: false }) as GlobalIssuesSearch
  const navigate = useNavigate()
  const filter = search.filter ?? 'assigned'
  const state = search.state ?? 'open'
  const [q, setQ] = useState('')
  const issues = useQuery({ ...myIssuesQuery(filter, state), placeholderData: (prev) => prev })
  const set = (s: GlobalIssuesSearch) => void navigate({ to: '/issues', search: { ...search, ...s } })
  const shown = useMemo(() => {
    const f = q.trim().toLowerCase()
    return (issues.data?.issues ?? []).filter((i) => !f || i.title.toLowerCase().includes(f) || i.repo.toLowerCase().includes(f))
  }, [issues.data, q])
  return (
    <Page title="Issues" icon={CircleDot} description={issues.data ? `${issues.data.total} total` : undefined}>
      <FilterBar className="px-5">
        <SegmentedControl label="Filter" value={filter} options={filters} onChange={(f) => set({ filter: f === 'assigned' ? undefined : f })} />
        <SegmentedControl label="State" value={state} options={states} onChange={(s) => set({ state: s === 'open' ? undefined : s })} />
        <SearchInput value={q} onChange={setQ} placeholder="Filter by title or repository…" label="Filter issues" shortcut />
      </FilterBar>
      {issues.isPending ? (
        <SkeletonRows rows={6} />
      ) : issues.isError ? (
        <ErrorState error={issues.error} />
      ) : shown.length === 0 ? (
        q ? (
          <EmptyState icon={<SearchX size={20} />} title="No matching issues" />
        ) : (
          <EmptyState icon={<Inbox size={20} />} title={filter === 'assigned' && state === 'open' ? 'Nothing assigned to you' : 'Nothing here'}>
            {state === 'open' ? emptyText[filter] : 'No matching issues.'}
          </EmptyState>
        )
      ) : (
        <IssueRows issues={shown} showRepo />
      )}
    </Page>
  )
}
