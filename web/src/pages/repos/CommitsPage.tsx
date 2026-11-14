import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { errorMessage, type Commit } from '../../api/client'
import { commitsQuery, repoQuery } from '../../api/queries'
import { RefPicker } from '../../components/code/RefPicker'
import { EmptyState } from '../../components/Page'
import { Alert, Button, Spinner, Time } from '../../components/ui'
import { useRepoName } from './RepoLayout'
import type { CommitsSearch } from './search'

function groupByDay(commits: Commit[]) {
  const groups: { day: string; commits: Commit[] }[] = []
  for (const c of commits) {
    const day = new Date(c.committer.date).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
    const last = groups[groups.length - 1]
    if (last?.day === day) last.commits.push(c)
    else groups.push({ day, commits: [c] })
  }
  return groups
}

export function CommitsPage() {
  const name = useRepoName()
  const search = useSearch({ strict: false }) as CommitsSearch
  const navigate = useNavigate()
  const repo = useQuery(repoQuery(name)).data!
  const ref = search.ref ?? repo.default_branch
  const page = search.page ?? 1
  const commits = useQuery({ ...commitsQuery(name, { ref, path: search.path, page }), enabled: !repo.empty })

  if (repo.empty) return <EmptyState title="No commits yet">Push to this repository to see its history.</EmptyState>
  const go = (p: number) => void navigate({ to: '/$repo/commits', params: { repo: name }, search: { ref, path: search.path, page: p > 1 ? p : undefined } })

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-3 p-4">
      <div className="flex items-center gap-3">
        <RefPicker repo={name} current={ref} onSelect={(r) => void navigate({ to: '/$repo/commits', params: { repo: name }, search: { ref: r, path: search.path } })} />
        {search.path && (
          <span className="text-fg-muted">
            History for <code className="font-mono text-fg">{search.path}</code>
          </span>
        )}
      </div>
      {commits.isPending ? (
        <Spinner />
      ) : commits.isError ? (
        <Alert>{errorMessage(commits.error)}</Alert>
      ) : (
        <>
          {groupByDay(commits.data.commits).map((g) => (
            <section key={g.day}>
              <h3 className="mb-1.5 text-sm font-medium text-fg-muted">Commits on {g.day}</h3>
              <ul className="overflow-hidden rounded-md border border-border">
                {g.commits.map((c) => (
                  <li key={c.sha} className="flex items-center gap-3 border-b border-border px-3 py-2 last:border-0 hover:bg-bg-subtle">
                    <div className="min-w-0 flex-1">
                      <Link to="/$repo/commit/$sha" params={{ repo: name, sha: c.sha }} className="block truncate font-medium hover:text-accent">
                        {c.subject}
                      </Link>
                      <div className="text-sm text-fg-faint">
                        {c.author.name} committed <Time iso={c.committer.date} />
                      </div>
                    </div>
                    <Link to="/$repo/commit/$sha" params={{ repo: name, sha: c.sha }} className="rounded border border-border px-1.5 py-0.5 font-mono text-sm text-fg-muted hover:text-accent">
                      {c.sha.slice(0, 7)}
                    </Link>
                    <Link to="/$repo" params={{ repo: name }} search={{ ref: c.sha }} className="text-sm text-fg-faint hover:text-accent" title="Browse files at this commit">
                      Browse
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          ))}
          <div className="flex justify-center gap-2">
            <Button disabled={page <= 1} onClick={() => go(page - 1)}>
              Newer
            </Button>
            <Button disabled={!commits.data.has_more} onClick={() => go(page + 1)}>
              Older
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
