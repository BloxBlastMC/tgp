import { useQuery } from '@tanstack/react-query'
import { Link, useSearch } from '@tanstack/react-router'
import { FileText } from 'lucide-react'
import { errorMessage } from '../../api/client'
import { blameQuery, repoQuery } from '../../api/queries'
import { Breadcrumbs } from '../../components/code/Breadcrumbs'
import { Alert, Button, Spinner, Time } from '../../components/ui'
import { useRepoName } from './RepoLayout'
import type { RefPathSearch } from './search'

export function BlamePage() {
  const name = useRepoName()
  const search = useSearch({ strict: false }) as RefPathSearch
  const repo = useQuery(repoQuery(name)).data!
  const ref = search.ref ?? repo.default_branch
  const path = search.path ?? ''
  const blame = useQuery({ ...blameQuery(name, { ref, path }), enabled: !!path })

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-3">
        <span className="rounded bg-bg-hover px-2 py-0.5 font-mono text-sm">{ref}</span>
        <Breadcrumbs repo={repo.name} gitRef={ref} path={path} />
        <div className="flex-1" />
        <Link to="/$repo/blob" params={{ repo: name }} search={{ ref, path }}>
          <Button variant="ghost">
            <FileText size={13} /> View file
          </Button>
        </Link>
      </div>
      {blame.isPending ? (
        <Spinner />
      ) : blame.isError ? (
        <Alert>{errorMessage(blame.error)}</Alert>
      ) : (
        <div className="overflow-x-auto rounded-md border border-border font-mono text-code">
          <table className="w-full border-collapse">
            <tbody>
              {blame.data.ranges.map((r, ri) =>
                r.lines.map((line, li) => (
                  <tr key={`${ri}-${li}`} className={li === 0 ? 'border-t border-border' : ''}>
                    <td className="w-72 max-w-72 border-r border-border bg-bg-subtle px-2 align-top font-sans text-sm">
                      {li === 0 && (
                        <div className="flex items-center gap-2 py-0.5">
                          <Link to="/$repo/commit/$sha" params={{ repo: name, sha: r.commit }} className="min-w-0 flex-1 truncate hover:text-accent" title={r.summary}>
                            {r.summary}
                          </Link>
                          <span className="shrink-0 text-fg-faint" title={r.author}>
                            <Time iso={r.date} />
                          </span>
                        </div>
                      )}
                    </td>
                    <td className="w-1 px-2 text-right text-fg-faint select-none">{r.start_line + li}</td>
                    <td className="pr-4 whitespace-pre">{line || ' '}</td>
                  </tr>
                )),
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
