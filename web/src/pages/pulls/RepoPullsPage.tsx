import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { GitPullRequest, Plus, Search } from 'lucide-react'
import { useEffect, useState } from 'react'
import { errorMessage } from '../../api/client'
import { pullsQuery, repoQuery } from '../../api/queries'
import { EmptyState } from '../../components/Page'
import { Alert, Button, Input, Spinner } from '../../components/ui'
import { useRepoName } from '../repos/RepoLayout'
import { PullRows, pullStates, Segmented, type PullListSearch } from './PullList'

export function RepoPullsPage() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const search = useSearch({ strict: false }) as PullListSearch
  const navigate = useNavigate()
  const state = search.state ?? 'open'
  const [q, setQ] = useState(search.q ?? '')
  useEffect(() => {
    const t = window.setTimeout(() => {
      if ((search.q ?? '') !== q) void navigate({ to: '/$repo/pulls', params: { repo: name }, search: { ...search, q: q || undefined }, replace: true })
    }, 250)
    return () => window.clearTimeout(t)
  }, [q, search, name, navigate])
  const pulls = useQuery(pullsQuery(name, { state, q: search.q }))
  const canWrite = repo.permission === 'write' || repo.permission === 'admin'
  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <Segmented
          label="State"
          value={state}
          options={pullStates}
          onChange={(s) => void navigate({ to: '/$repo/pulls', params: { repo: name }, search: { ...search, state: s === 'open' ? undefined : s } })}
        />
        <div className="relative w-64">
          <Search size={13} className="absolute top-1/2 left-2 -translate-y-1/2 text-fg-faint" />
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search titles" className="h-7 pl-7" aria-label="Search pull requests" />
        </div>
        <div className="flex-1" />
        {pulls.data && <span className="text-[12px] text-fg-faint">{pulls.data.total} total</span>}
        {canWrite && !repo.empty && (
          <Link to="/$repo/compare/$" params={{ repo: name, _splat: '' }}>
            <Button variant="primary">
              <Plus size={14} /> New pull request
            </Button>
          </Link>
        )}
      </div>
      {pulls.isPending ? (
        <Spinner />
      ) : pulls.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(pulls.error)}</Alert>
        </div>
      ) : pulls.data.pulls.length === 0 ? (
        <EmptyState title={search.q ? 'No matching pull requests' : `No ${state === 'all' ? '' : state + ' '}pull requests`}>
          <GitPullRequest size={20} className="mx-auto my-2 text-fg-faint" />
          {canWrite && state === 'open' && !search.q && 'Push a branch and open a pull request to propose changes.'}
        </EmptyState>
      ) : (
        <PullRows pulls={pulls.data.pulls} />
      )}
    </div>
  )
}
