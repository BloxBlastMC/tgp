import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Archive, FolderGit2, GitBranch, Plus, SearchX } from 'lucide-react'
import { useMemo, useState } from 'react'
import { reposQuery, sessionQuery } from '../../api/queries'
import { EmptyState, Page } from '../../components/Page'
import { CreateRepoDialog } from '../../components/repos/CreateRepoDialog'
import { Badge, Button, ErrorState, FilterBar, FilterMenu, List, SearchInput, Sep, SkeletonRows, Switch, Time } from '../../components/ui'

type Sort = 'pushed' | 'name'

const permTone = { admin: 'warning', write: 'accent', triage: 'neutral', read: 'neutral' } as const

export function ReposPage() {
  const repos = useQuery(reposQuery)
  const isAdmin = useQuery(sessionQuery).data?.user.role === 'admin'
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState<Sort>('pushed')
  const [showArchived, setShowArchived] = useState(true)
  const [creating, setCreating] = useState(false)
  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase()
    const list = (repos.data ?? []).filter(
      (r) => (showArchived || !r.archived) && (!f || r.name.toLowerCase().includes(f) || r.description.toLowerCase().includes(f)),
    )
    if (sort === 'name') return [...list].sort((a, b) => a.name.localeCompare(b.name))
    return list
  }, [repos.data, filter, sort, showArchived])
  const archivedCount = repos.data?.filter((r) => r.archived).length ?? 0

  return (
    <Page
      title="Repositories"
      description={repos.data ? `${repos.data.length} total` : undefined}
      actions={
        isAdmin && (
          <Button variant="primary" icon={<Plus size={14} />} onClick={() => setCreating(true)}>
            New repository
          </Button>
        )
      }
    >
      {repos.isError ? (
        <ErrorState error={repos.error} />
      ) : repos.data?.length === 0 ? (
        <EmptyState
          icon={<FolderGit2 size={20} />}
          title="No repositories yet"
          action={
            isAdmin && (
              <Button variant="primary" onClick={() => setCreating(true)}>
                Create the first repository
              </Button>
            )
          }
        >
          {!isAdmin && "You don't have access to any repositories yet. Ask an admin to grant you access."}
        </EmptyState>
      ) : (
        <>
          <FilterBar
            end={
              archivedCount > 0 && (
                <div className="w-40">
                  <Switch checked={showArchived} onChange={setShowArchived} label={<span className="text-sm font-normal text-fg-muted">Show archived</span>} />
                </div>
              )
            }
          >
            <SearchInput value={filter} onChange={setFilter} label="Filter repositories" placeholder="Filter repositories…" shortcut />
            <FilterMenu
              label="Sort"
              value={sort}
              onChange={setSort}
              options={[
                { value: 'pushed', label: 'Last pushed' },
                { value: 'name', label: 'Name' },
              ]}
            />
          </FilterBar>
          <div className="mx-auto max-w-5xl px-6 py-5">
            {repos.isPending ? (
              <List>
                <SkeletonRows rows={6} />
              </List>
            ) : shown.length === 0 ? (
              <EmptyState size="inline" icon={<SearchX size={18} />} title="No matching repositories">
                Try a different search.
              </EmptyState>
            ) : (
              <List>
                {shown.map((r) => (
                  <li key={r.id} className="relative flex items-center gap-3 border-b border-border px-4 py-3 transition-colors last:border-0 hover:bg-bg-hover/60">
                    <span
                      className="flex size-8 shrink-0 items-center justify-center rounded-md text-sm font-semibold text-white uppercase"
                      style={{ backgroundColor: `var(--avatar-${(r.id % 8) + 1})` }}
                      aria-hidden
                    >
                      {r.name.slice(0, 1)}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <Link to="/$repo" params={{ repo: r.name }} className="truncate font-semibold after:absolute after:inset-0 hover:text-accent">
                          {r.name}
                        </Link>
                        {r.archived && (
                          <Badge tone="warning" icon={<Archive size={10} />}>
                            archived
                          </Badge>
                        )}
                        {r.empty && <Badge>empty</Badge>}
                      </div>
                      <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-sm text-fg-muted">
                        {r.description ? <span className="truncate">{r.description}</span> : <span className="text-fg-faint">No description</span>}
                      </div>
                    </div>
                    <div className="hidden shrink-0 items-center gap-2 text-sm text-fg-faint md:flex">
                      <span className="inline-flex items-center gap-1 font-mono">
                        <GitBranch size={12} /> {r.default_branch}
                      </span>
                      <Sep />
                      <span>{r.pushed_at ? <>Pushed <Time iso={r.pushed_at} /></> : 'No pushes yet'}</span>
                    </div>
                    <Badge tone={permTone[r.permission as keyof typeof permTone] ?? 'neutral'} className="shrink-0">
                      {r.permission}
                    </Badge>
                  </li>
                ))}
              </List>
            )}
          </div>
        </>
      )}
      {isAdmin && <CreateRepoDialog open={creating} onClose={() => setCreating(false)} />}
    </Page>
  )
}
