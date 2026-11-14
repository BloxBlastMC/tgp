import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { errorMessage } from '../../api/client'
import { myPullsQuery, type MyPullsFilter, type PullState } from '../../api/queries'
import { EmptyState, Page } from '../../components/Page'
import { Alert, Spinner } from '../../components/ui'
import { PullRows, pullStates, Segmented } from './PullList'

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
  const pulls = useQuery(myPullsQuery(filter, state))
  const set = (s: GlobalPullsSearch) => void navigate({ to: '/pulls', search: { ...search, ...s } })
  return (
    <Page title="Pull requests">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <Segmented label="Filter" value={filter} options={filters} onChange={(f) => set({ filter: f === 'review_requested' ? undefined : f })} />
        <Segmented label="State" value={state} options={pullStates} onChange={(s) => set({ state: s === 'open' ? undefined : s })} />
        <div className="flex-1" />
        {pulls.data && <span className="text-sm text-fg-faint">{pulls.data.total} total</span>}
      </div>
      {pulls.isPending ? (
        <Spinner />
      ) : pulls.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(pulls.error)}</Alert>
        </div>
      ) : pulls.data.pulls.length === 0 ? (
        <EmptyState title="Nothing here">{state === 'open' ? emptyText[filter] : 'No matching pull requests.'}</EmptyState>
      ) : (
        <PullRows pulls={pulls.data.pulls} showRepo />
      )}
    </Page>
  )
}
