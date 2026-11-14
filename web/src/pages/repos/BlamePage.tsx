import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Code2, History, UserSquare } from 'lucide-react'
import { blameQuery, repoQuery } from '../../api/queries'
import { PathBreadcrumbs } from '../../components/code/Breadcrumbs'
import { RefPicker } from '../../components/code/RefPicker'
import { Avatar, ButtonLink, CardHeaderBar, ErrorState, SegmentedControl, Skeleton, Time } from '../../components/ui'
import { useRepoName } from './RepoLayout'
import type { RefPathSearch } from './search'

/** Older lines fade toward the background; recent ones stand out. */
function ageTone(date: string, newest: number, oldest: number): number {
  if (newest === oldest) return 1
  const t = (new Date(date).getTime() - oldest) / (newest - oldest)
  return 0.25 + 0.75 * t
}

export function BlamePage() {
  const name = useRepoName()
  const search = useSearch({ strict: false }) as RefPathSearch
  const navigate = useNavigate()
  const repo = useQuery(repoQuery(name)).data!
  const ref = search.ref ?? repo.default_branch
  const path = search.path ?? ''
  const blame = useQuery({ ...blameQuery(name, { ref, path }), enabled: !!path })
  const times = (blame.data?.ranges ?? []).map((r) => new Date(r.date).getTime())
  const newest = Math.max(...times, 0)
  const oldest = Math.min(...times, newest)

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-4 px-6 py-5">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repo={name} current={ref} onSelect={(r) => void navigate({ to: '/$repo/blame', params: { repo: name }, search: { ref: r, path } })} />
        <PathBreadcrumbs repo={repo.name} gitRef={ref} path={path} />
        <div className="flex-1" />
        <ButtonLink to="/$repo/commits" params={{ repo: name }} search={{ ref, path }} variant="ghost" icon={<History size={14} />}>
          History
        </ButtonLink>
      </div>
      {blame.isError ? (
        <ErrorState error={blame.error} className="p-0" />
      ) : (
        <div className="overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
          <CardHeaderBar className="px-4 text-fg-muted">
            <SegmentedControl
              size="sm"
              label="View"
              value="blame"
              onChange={(v) => v === 'code' && void navigate({ to: '/$repo/blob', params: { repo: name }, search: { ref, path } })}
              options={[
                { value: 'code', label: 'Code', icon: <Code2 size={12} /> },
                { value: 'blame', label: 'Blame', icon: <UserSquare size={12} /> },
              ]}
            />
            <span className="flex items-center gap-1.5 text-xs">
              Older
              <span className="flex gap-0.5" aria-hidden>
                {[0.25, 0.45, 0.65, 0.85, 1].map((o) => (
                  <span key={o} className="h-2 w-3 rounded-sm bg-accent" style={{ opacity: o }} />
                ))}
              </span>
              Newer
            </span>
          </CardHeaderBar>
          {blame.isPending ? (
            <div className="flex flex-col gap-2 p-4" role="status" aria-label="Loading">
              {Array.from({ length: 8 }, (_, i) => (
                <Skeleton key={i} className="h-3" style={{ width: `${30 + ((i * 23) % 55)}%` }} />
              ))}
            </div>
          ) : (
            <div className="overflow-x-auto font-mono text-code">
              <table className="w-full border-collapse">
                <tbody>
                  {blame.data.ranges.map((r, ri) =>
                    r.lines.map((line, li) => (
                      <tr key={`${ri}-${li}`} className={li === 0 && ri > 0 ? 'border-t border-border' : ''}>
                        <td className="relative w-80 max-w-80 border-r border-border bg-bg-subtle pr-3 pl-4 align-top font-sans text-sm">
                          <span className="absolute inset-y-0 left-0 w-0.5 bg-accent" style={{ opacity: ageTone(r.date, newest, oldest) }} aria-hidden />
                          {li === 0 && (
                            <div className="flex items-center gap-2 py-1">
                              <Avatar user={{ display_name: r.author, username: r.author }} size={16} />
                              <Link to="/$repo/commit/$sha" params={{ repo: name, sha: r.commit }} className="min-w-0 flex-1 truncate hover:text-accent" title={`${r.summary} — ${r.author}`}>
                                {r.summary}
                              </Link>
                              <span className="shrink-0 text-fg-faint">
                                <Time iso={r.date} />
                              </span>
                            </div>
                          )}
                        </td>
                        <td className="w-1 px-3 text-right text-fg-faint tabular-nums select-none">{r.start_line + li}</td>
                        <td className="pr-4 whitespace-pre">{line || ' '}</td>
                      </tr>
                    )),
                  )}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
