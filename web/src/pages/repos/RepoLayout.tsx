import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useParams } from '@tanstack/react-router'
import { Archive, Boxes, CircleDot, Code2, FolderX, GitBranch, GitPullRequest, History, Package, Play, Settings } from 'lucide-react'
import { useEffect } from 'react'
import { ApiRequestError } from '../../api/client'
import { repoQuery } from '../../api/queries'
import { CloneButton } from '../../components/code/CloneButton'
import { Badge, ButtonLink, EmptyState, ErrorState, NavTabs, Skeleton, type NavTab } from '../../components/ui'
import { canWrite } from '../../lib/permissions'
import { recordVisit } from '../../lib/recent'

export function useRepoName(): string {
  return (useParams({ strict: false }) as { repo: string }).repo
}

/** The current repository from the cache RepoLayout already loaded. */
export function useRepo() {
  const name = useRepoName()
  return useQuery(repoQuery(name))
}

const permTone = { admin: 'warning', write: 'accent', triage: 'neutral', read: 'neutral' } as const

export function RepoLayout() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name))
  const resolved = repo.data?.name
  useEffect(() => {
    if (resolved) recordVisit(resolved)
  }, [resolved])

  if (repo.isPending)
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="shrink-0 border-b border-border px-5 pt-3">
          <Skeleton className="h-4 w-56" />
          <div className="mt-4 flex gap-4 pb-3">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-3 w-16" />
            ))}
          </div>
        </div>
      </div>
    )
  if (repo.isError) {
    if (repo.error instanceof ApiRequestError && repo.error.status === 404)
      return (
        <EmptyState
          icon={<FolderX size={20} />}
          title="Repository not found"
          action={
            <ButtonLink to="/repos" variant="secondary">
              All repositories
            </ButtonLink>
          }
        >
          It doesn't exist or you don't have access.
        </EmptyState>
      )
    return <ErrorState error={repo.error} />
  }
  const r = repo.data
  const tabs: NavTab[] = [
    { to: '/$repo', label: 'Code', icon: Code2, exact: true },
    { to: '/$repo/issues', label: 'Issues', icon: CircleDot },
    { to: '/$repo/pulls', label: 'Pull requests', icon: GitPullRequest },
    { to: '/$repo/ci', label: 'CI', icon: Play },
    { to: '/$repo/releases', label: 'Releases', icon: Package },
    { to: '/$repo/packages', label: 'Packages', icon: Boxes },
    { to: '/$repo/commits', label: 'Commits', icon: History },
    { to: '/$repo/branches', label: 'Branches', icon: GitBranch },
  ]
  if (canWrite(r.permission)) tabs.push({ to: '/$repo/settings', label: 'Settings', icon: Settings })
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="shrink-0 border-b border-border bg-bg-sidebar/40 px-5 pt-2.5">
        <div className="flex min-h-8 items-center gap-3">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <span
              className="flex size-6 shrink-0 items-center justify-center rounded-md text-xs font-semibold text-white uppercase"
              style={{ backgroundColor: `var(--avatar-${(r.id % 8) + 1})` }}
              aria-hidden
            >
              {r.name.slice(0, 1)}
            </span>
            <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5 text-md">
              <Link to="/repos" className="text-fg-muted hover:text-fg">
                Repositories
              </Link>
              <span className="text-fg-faint" aria-hidden>
                /
              </span>
              <Link to="/$repo" params={{ repo: r.name }} className="truncate font-semibold hover:text-accent">
                {r.name}
              </Link>
            </nav>
            {r.archived && (
              <Badge tone="warning" icon={<Archive size={10} />}>
                archived
              </Badge>
            )}
            <Badge tone={permTone[r.permission as keyof typeof permTone] ?? 'neutral'} title={`Your access: ${r.permission}`}>
              {r.permission}
            </Badge>
            {r.description && <span className="hidden min-w-0 truncate text-sm text-fg-muted lg:block">— {r.description}</span>}
          </div>
          <CloneButton url={r.clone_url} name={r.name} />
        </div>
        <NavTabs items={tabs.map((t) => ({ ...t, params: { repo: r.name } }))} className="mt-1.5" label="Repository" />
      </header>
      <div className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </div>
  )
}
