import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useParams } from '@tanstack/react-router'
import { Archive, CircleDot, Code2, GitBranch, GitPullRequest, History, Package, Play, Settings } from 'lucide-react'
import type { ComponentType } from 'react'
import { ApiRequestError, errorMessage } from '../../api/client'
import { repoQuery } from '../../api/queries'
import { CloneButton } from '../../components/code/CloneButton'
import { EmptyState } from '../../components/Page'
import { Alert, Badge, Spinner } from '../../components/ui'

type Tab = { to: string; label: string; icon: ComponentType<{ size?: number }>; exact?: boolean; minPerm?: 'write' }

const tabs: Tab[] = [
  { to: '/$repo', label: 'Code', icon: Code2, exact: true },
  { to: '/$repo/issues', label: 'Issues', icon: CircleDot },
  { to: '/$repo/pulls', label: 'Pull requests', icon: GitPullRequest },
  { to: '/$repo/ci', label: 'CI', icon: Play },
  { to: '/$repo/releases', label: 'Releases', icon: Package },
  { to: '/$repo/commits', label: 'Commits', icon: History },
  { to: '/$repo/branches', label: 'Branches', icon: GitBranch },
  { to: '/$repo/settings', label: 'Settings', icon: Settings, minPerm: 'write' },
]

export function useRepoName(): string {
  return (useParams({ strict: false }) as { repo: string }).repo
}

export function RepoLayout() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name))
  if (repo.isPending) return <Spinner />
  if (repo.isError) {
    if (repo.error instanceof ApiRequestError && repo.error.status === 404)
      return <EmptyState title="Repository not found">It doesn't exist or you don't have access.</EmptyState>
    return (
      <div className="p-4">
        <Alert>{errorMessage(repo.error)}</Alert>
      </div>
    )
  }
  const r = repo.data
  const canWrite = r.permission === 'write' || r.permission === 'admin'
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="shrink-0 border-b border-border px-4 pt-2.5">
        <div className="flex items-center gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <Link to="/$repo" params={{ repo: r.name }} className="text-lg font-semibold hover:underline">
                {r.name}
              </Link>
              {r.archived && (
                <Badge tone="warning">
                  <Archive size={10} className="mr-1" /> archived
                </Badge>
              )}
              <Badge>{r.permission}</Badge>
            </div>
            {r.description && <p className="truncate text-fg-muted">{r.description}</p>}
          </div>
          <CloneButton url={r.clone_url} name={r.name} />
        </div>
        <nav className="mt-2 -mb-px flex gap-1">
          {tabs
            .filter((t) => !t.minPerm || canWrite)
            .map(({ to, label, icon: Icon, exact }) => (
              <Link
                key={to}
                to={to}
                params={{ repo: r.name }}
                activeOptions={{ exact, includeSearch: false }}
                className="flex h-8 items-center gap-1.5 border-b-2 border-transparent px-2 text-fg-muted hover:text-fg data-[status=active]:border-accent data-[status=active]:text-fg"
              >
                <Icon size={14} /> {label}
              </Link>
            ))}
        </nav>
      </header>
      <div className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </div>
  )
}
