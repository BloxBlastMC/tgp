import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { allRunsQuery } from '../../api/queries'
import { Play } from 'lucide-react'
import { Pager, RunRows } from '../../components/ci/common'
import { EmptyState, Page } from '../../components/Page'
import { ErrorState, FilterBar, SegmentedControl, SkeletonRows } from '../../components/ui'

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
    placeholderData: (prev) => prev,
  })
  return (
    <Page title="Runs" icon={Play} description={runs.data ? `${runs.data.total} runs` : undefined}>
      <FilterBar className="px-5">
        <SegmentedControl
          label="Runs"
          value={search.scope ?? 'all'}
          options={[
            { value: 'all', label: 'All' },
            { value: 'active', label: 'Active' },
          ]}
          onChange={(v) => set({ scope: v === 'active' ? 'active' : undefined })}
        />
        <SegmentedControl
          label="Actor"
          value={search.mine ? 'mine' : 'everyone'}
          options={[
            { value: 'everyone', label: 'Everyone' },
            { value: 'mine', label: 'Mine' },
          ]}
          onChange={(v) => set({ mine: v === 'mine' ? true : undefined })}
        />
      </FilterBar>
      {runs.isPending ? (
        <SkeletonRows rows={6} />
      ) : runs.isError ? (
        <ErrorState error={runs.error} />
      ) : runs.data.runs.length === 0 ? (
        <EmptyState icon={<Play size={20} />} title={search.scope === 'active' ? 'Nothing running' : 'No workflow runs yet'}>
          {search.scope === 'active' ? 'No runs are queued or in progress.' : 'Add a workflow file under .tgp/workflows/ in a repository and push to start a run.'}
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
