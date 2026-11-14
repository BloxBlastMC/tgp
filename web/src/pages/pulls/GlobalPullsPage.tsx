import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { GitPullRequest, Inbox, SearchX } from 'lucide-react'
import { useMemo, useState } from 'react'
import { myPullsQuery, type MyPullsFilter, type PullState } from '../../api/queries'
import { EmptyState, Page } from '../../components/Page'
import { ErrorState, FilterBar, SearchInput, SegmentedControl, SkeletonRows } from '../../components/ui'
import { PullRows, pullStates } from './PullList'

export type GlobalPullsSearch = { filter?: MyPullsFilter; state?: PullState }

export function validateGlobalPulls(s: Record<string, unknown>): GlobalPullsSearch {
  const out: GlobalPullsSearch = {}
  if (s.filter === 'authored' || s.filter === 'all') out.filter = s.filter
  if (typeof s.state === 'string' && ['closed', 'merged', 'all'].includes(s.state)) out.state = s.state as PullState
  return out
}

const filters: { value: MyPullsFilter; label: string }[] = [
  { value: 'review_requested', label: 'Review requested' },
  { value: 'authored', label: 'Created by me' },
  { value: 'all', label: 'All' },
]

const emptyText: Record<MyPullsFilter, string> = {
  review_requested: 'Nobody is waiting for your review.',
  authored: "You haven't opened any pull requests.",
  all: 'No pull requests in the repositories you can access.',
}

export function GlobalPullsPage() {
  const search = useSearch({ strict: false }) as GlobalPullsSearch
  const navigate = useNavigate()
  const filter = search.filter ?? 'review_requested'
  const state = search.state ?? 'open'
  const [q, setQ] = useState('')
  const pulls = useQuery({ ...myPullsQuery(filter, state), placeholderData: (prev) => prev })
  const set = (s: GlobalPullsSearch) => void navigate({ to: '/pulls', search: { ...search, ...s } })
  const shown = useMemo(() => {
    const f = q.trim().toLowerCase()
    return (pulls.data?.pulls ?? []).filter((p) => !f || p.title.toLowerCase().includes(f) || p.repo.toLowerCase().includes(f) || `#${p.number}` === f)
  }, [pulls.data, q])
  return (
    <Page title="Pull requests" icon={GitPullRequest} description={pulls.data ? `${pulls.data.total} total` : undefined}>
      <FilterBar className="px-5">
        <SegmentedControl label="Filter" value={filter} options={filters} onChange={(f) => set({ filter: f === 'review_requested' ? undefined : f })} />
        <SegmentedControl label="State" value={state} options={pullStates} onChange={(s) => set({ state: s === 'open' ? undefined : s })} />
        <SearchInput value={q} onChange={setQ} placeholder="Filter by title or repository…" label="Filter pull requests" shortcut />
      </FilterBar>
      {pulls.isPending ? (
        <SkeletonRows rows={6} />
      ) : pulls.isError ? (
        <ErrorState error={pulls.error} />
      ) : shown.length === 0 ? (
        q ? (
          <EmptyState icon={<SearchX size={20} />} title="No matching pull requests" />
        ) : (
          <EmptyState icon={<Inbox size={20} />} title={filter === 'review_requested' && state === 'open' ? "You're all caught up" : 'Nothing here'}>
            {state === 'open' ? emptyText[filter] : 'No matching pull requests.'}
          </EmptyState>
        )
      ) : (
        <PullRows pulls={shown} showRepo />
      )}
    </Page>
  )
}
