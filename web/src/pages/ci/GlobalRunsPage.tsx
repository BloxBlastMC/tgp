import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { errorMessage } from '../../api/client'
import { allRunsQuery } from '../../api/queries'
import { Pager, RunRows } from '../../components/ci/common'
import { EmptyState, Page } from '../../components/Page'
import { Alert, Spinner } from '../../components/ui'
import { Segmented } from '../pulls/PullList'

export type GlobalRunsSearch = { scope?: 'active'; mine?: true; page?: number }

export function validateGlobalRuns(s: Record<string, unknown>): GlobalRunsSearch {
  const out: GlobalRunsSearch = {}
  if (s.scope === 'active') out.scope = 'active'
  if (s.mine === true || s.mine === 'true' || s.mine === '1') out.mine = true
  const p = Number(s.page)
  if (Number.isInteger(p) && p > 1) out.page = p
  return out
}

const PER_PAGE = 50

export function GlobalRunsPage() {
  const search = useSearch({ strict: false }) as GlobalRunsSearch
  const navigate = useNavigate()
  const set = (s: Partial<GlobalRunsSearch>) => void navigate({ to: '/ci', search: { ...search, page: undefined, ...s } })
  const page = search.page ?? 1
  const runs = useQuery({
    ...allRunsQuery({ status: search.scope === 'active' ? 'active' : undefined, actor: search.mine ? '@me' : undefined, page }),
    refetchInterval: (q) => (q.state.data?.runs.some((r) => r.status !== 'completed') ? 5000 : 30_000),
  })
  return (
    <Page title="CI">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <Segmented
          label="Runs"
          value={search.scope ?? 'all'}
          options={[
            { value: 'all', label: 'All' },
            { value: 'active', label: 'Active' },
          ]}
          onChange={(v) => set({ scope: v === 'active' ? 'active' : undefined })}
        />
        <Segmented
          label="Actor"
          value={search.mine ? 'mine' : 'everyone'}
          options={[
            { value: 'everyone', label: 'Everyone' },
            { value: 'mine', label: 'Mine' },
          ]}
          onChange={(v) => set({ mine: v === 'mine' ? true : undefined })}
        />
        <div className="flex-1" />
        {runs.data && <span className="text-[12px] text-fg-faint">{runs.data.total} runs</span>}
      </div>
      {runs.isPending ? (
        <Spinner />
      ) : runs.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(runs.error)}</Alert>
        </div>
      ) : runs.data.runs.length === 0 ? (
        <EmptyState title={search.scope === 'active' ? 'Nothing running' : 'No workflow runs yet'}>
          {search.scope === 'active' ? 'No runs are queued or in progress.' : 'Add a workflow under .tgp/workflows/ in a repository to start using CI.'}
        </EmptyState>
      ) : (
        <>
          <RunRows runs={runs.data.runs} showRepo />
          <Pager page={page} total={runs.data.total} perPage={PER_PAGE} onPage={(p) => void navigate({ to: '/ci', search: { ...search, page: p > 1 ? p : undefined } })} />
        </>
      )}
    </Page>
  )
}
