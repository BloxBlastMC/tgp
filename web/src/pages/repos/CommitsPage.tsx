import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { History, X } from 'lucide-react'
import { commitsQuery, repoQuery } from '../../api/queries'
import { CommitList } from '../../components/code/CommitList'
import { RefPicker } from '../../components/code/RefPicker'
import { EmptyState } from '../../components/Page'
import { ErrorState, Pagination, Skeleton } from '../../components/ui'
import { useRepoName } from './RepoLayout'
import type { CommitsSearch } from './search'

export function CommitsPage() {
  const name = useRepoName()
  const search = useSearch({ strict: false }) as CommitsSearch
  const navigate = useNavigate()
  const repo = useQuery(repoQuery(name)).data!
  const ref = search.ref ?? repo.default_branch
  const page = search.page ?? 1
  const commits = useQuery({ ...commitsQuery(name, { ref, path: search.path, page }), enabled: !repo.empty })

  if (repo.empty)
    return (
      <EmptyState icon={<History size={20} />} title="No commits yet">
        Push to this repository to see its history.
      </EmptyState>
    )
  const go = (p: number) => void navigate({ to: '/$repo/commits', params: { repo: name }, search: { ref, path: search.path, page: p > 1 ? p : undefined } })

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-4 px-6 py-5">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repo={name} current={ref} onSelect={(r) => void navigate({ to: '/$repo/commits', params: { repo: name }, search: { ref: r, path: search.path } })} />
        {search.path && (
          <span className="flex min-w-0 items-center gap-1.5 rounded-md border border-border bg-bg-subtle py-0.5 pr-1 pl-2 text-sm text-fg-muted">
            History for <code className="truncate font-mono text-fg">{search.path}</code>
            <Link to="/$repo/commits" params={{ repo: name }} search={{ ref }} aria-label="Show all history" className="rounded p-0.5 hover:bg-bg-hover hover:text-fg">
              <X size={12} />
            </Link>
          </span>
        )}
      </div>
      {commits.isPending ? (
        <div className="overflow-hidden rounded-lg border border-border" role="status" aria-label="Loading">
          {Array.from({ length: 6 }, (_, i) => (
            <div key={i} className="flex items-center gap-3 border-b border-border px-4 py-3 last:border-0">
              <Skeleton className="size-5 rounded-full" />
              <div className="flex flex-1 flex-col gap-1.5">
                <Skeleton className="h-3 w-1/2" />
                <Skeleton className="h-2.5 w-1/4" />
              </div>
              <Skeleton className="h-4 w-16" />
            </div>
          ))}
        </div>
      ) : commits.isError ? (
        <ErrorState error={commits.error} className="p-0" />
      ) : commits.data.commits.length === 0 ? (
        <EmptyState size="inline" icon={<History size={18} />} title="No commits">
          Nothing touched {search.path ? <code className="font-mono">{search.path}</code> : 'this ref'} yet.
        </EmptyState>
      ) : (
        <>
          <CommitList repo={name} commits={commits.data.commits} />
          <Pagination page={page} hasNext={commits.data.has_more} onPage={go} prevLabel="Newer" nextLabel="Older" className="px-0" />
        </>
      )}
    </div>
  )
}
