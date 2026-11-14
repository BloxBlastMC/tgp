import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { errorMessage } from '../../api/client'
import { myIssuesQuery, type MyIssuesFilter } from '../../api/queries'
import { EmptyState, Page } from '../../components/Page'
import { Alert, Spinner } from '../../components/ui'
import { Segmented } from '../pulls/PullList'
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
  const issues = useQuery(myIssuesQuery(filter, state))
  const set = (s: GlobalIssuesSearch) => void navigate({ to: '/issues', search: { ...search, ...s } })
  return (
    <Page title="Issues">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <Segmented label="Filter" value={filter} options={filters} onChange={(f) => set({ filter: f === 'assigned' ? undefined : f })} />
        <Segmented label="State" value={state} options={states} onChange={(s) => set({ state: s === 'open' ? undefined : s })} />
        <div className="flex-1" />
        {issues.data && <span className="text-sm text-fg-faint">{issues.data.total} total</span>}
      </div>
      {issues.isPending ? (
        <Spinner />
      ) : issues.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(issues.error)}</Alert>
        </div>
      ) : issues.data.issues.length === 0 ? (
        <EmptyState title="Nothing here">{state === 'open' ? emptyText[filter] : 'No matching issues.'}</EmptyState>
      ) : (
        <IssueRows issues={issues.data.issues} showRepo />
      )}
    </Page>
  )
}
