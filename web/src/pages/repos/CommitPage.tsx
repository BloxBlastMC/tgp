import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { errorMessage } from '../../api/client'
import { commitQuery } from '../../api/queries'
import { DiffView } from '../../components/code/DiffView'
import { Alert, Button, Spinner, Time, useCopy } from '../../components/ui'
import { useRepoName } from './RepoLayout'

export function CommitPage() {
  const name = useRepoName()
  const { sha } = useParams({ strict: false }) as { sha: string }
  const q = useQuery(commitQuery(name, sha))
  const [copied, copy] = useCopy()
  if (q.isPending) return <Spinner />
  if (q.isError)
    return (
      <div className="p-4">
        <Alert>{errorMessage(q.error)}</Alert>
      </div>
    )
  const { commit: c, diff } = q.data
  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-4 p-4">
      <div className="rounded-md border border-border">
        <div className="px-4 py-3">
          <h2 className="text-[15px] font-semibold">{c.subject}</h2>
          {c.body && <pre className="mt-2 font-sans text-[13px] whitespace-pre-wrap text-fg-muted">{c.body}</pre>}
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-border bg-bg-subtle px-4 py-2 text-[12px] text-fg-muted">
          <span>
            <span className="font-medium text-fg">{c.author.name}</span> authored <Time iso={c.author.date} />
            {c.committer.name !== c.author.name && (
              <>
                {' '}
                · <span className="font-medium text-fg">{c.committer.name}</span> committed <Time iso={c.committer.date} />
              </>
            )}
          </span>
          <div className="flex-1" />
          {c.parents.length > 0 && (
            <span>
              {c.parents.length === 1 ? 'parent ' : 'parents '}
              {c.parents.map((p) => (
                <Link key={p} to="/$repo/commit/$sha" params={{ repo: name, sha: p }} className="mr-1 font-mono text-accent hover:underline">
                  {p.slice(0, 7)}
                </Link>
              ))}
            </span>
          )}
          <button type="button" onClick={() => copy(c.sha)} className="font-mono hover:text-fg" title="Copy full SHA">
            {copied ? 'copied' : c.sha.slice(0, 12)}
          </button>
          <Link to="/$repo" params={{ repo: name }} search={{ ref: c.sha }}>
            <Button variant="ghost">Browse files</Button>
          </Link>
        </div>
      </div>
      <DiffView diff={diff} />
    </div>
  )
}
