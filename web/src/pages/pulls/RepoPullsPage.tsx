import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { GitPullRequest, Plus, SearchX } from 'lucide-react'
import { pullsQuery, repoQuery } from '../../api/queries'
import { EmptyState } from '../../components/Page'
import { ButtonLink, ErrorState, FilterBar, Pagination, SearchInput, SegmentedControl, SkeletonRows } from '../../components/ui'
import { canWrite as canWritePerm } from '../../lib/permissions'
import { useRepoName } from '../repos/RepoLayout'
import { PullRows, pullStates, type PullListSearch } from './PullList'

export function RepoPullsPage() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const search = useSearch({ strict: false }) as PullListSearch
  const navigate = useNavigate()
  const state = search.state ?? 'open'
  const page = search.page ?? 1
  const set = (s: Partial<PullListSearch>, replace = false) =>
    void navigate({ to: '/$repo/pulls', params: { repo: name }, search: { ...search, page: undefined, ...s }, replace })
  const pulls = useQuery({ ...pullsQuery(name, { state, q: search.q, page }), placeholderData: (prev) => prev })
  const canWrite = canWritePerm(repo.permission)
  return (
    <div className="flex flex-col">
      <FilterBar
        className="px-5"
        end={
          <>
            {pulls.data && <span className="text-sm text-fg-faint tabular-nums">{pulls.data.total} total</span>}
            {canWrite && !repo.empty && !repo.archived && (
              <ButtonLink to="/$repo/compare/$" params={{ repo: name, _splat: '' }} variant="primary" icon={<Plus size={14} />}>
                New pull request
              </ButtonLink>
            )}
          </>
        }
      >
        <SegmentedControl label="State" value={state} options={pullStates} onChange={(s) => set({ state: s === 'open' ? undefined : s })} />
        <SearchInput value={search.q ?? ''} onChange={(q) => set({ q: q || undefined }, true)} debounce={250} placeholder="Search titles…" label="Search pull requests" shortcut />
      </FilterBar>
      {pulls.isPending ? (
        <SkeletonRows rows={6} />
      ) : pulls.isError ? (
        <ErrorState error={pulls.error} />
      ) : pulls.data.pulls.length === 0 ? (
        search.q ? (
          <EmptyState icon={<SearchX size={20} />} title="No matching pull requests">
            Nothing matches “{search.q}”.
          </EmptyState>
        ) : (
          <EmptyState
            icon={<GitPullRequest size={20} />}
            title={`No ${state === 'all' ? '' : state + ' '}pull requests`}
            action={
              canWrite &&
              state === 'open' &&
              !repo.empty && (
                <ButtonLink to="/$repo/compare/$" params={{ repo: name, _splat: '' }} variant="secondary" icon={<Plus size={14} />}>
                  New pull request
                </ButtonLink>
              )
            }
          >
            {canWrite && state === 'open' && 'Push a branch and open a pull request to propose changes.'}
          </EmptyState>
        )
      ) : (
        <>
          <PullRows pulls={pulls.data.pulls} />
          <Pagination page={page} total={pulls.data.total} perPage={50} onPage={(p) => set({ page: p > 1 ? p : undefined })} />
        </>
      )}
    </div>
  )
}
