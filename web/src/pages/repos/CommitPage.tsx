import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { FolderTree, GitCommit } from 'lucide-react'
import { commitQuery } from '../../api/queries'
import { commitAuthor } from '../../components/code/CommitList'
import { DiffView } from '../../components/code/DiffView'
import { Avatar, ButtonLink, CopyButton, ErrorState, PageSkeleton, Time } from '../../components/ui'
import { shortSha } from '../../lib/format'
import { Container } from '../../components/Page'
import { useRepoName } from './RepoLayout'

export function CommitPage() {
  const name = useRepoName()
  const { sha } = useParams({ strict: false }) as { sha: string }
  const q = useQuery(commitQuery(name, sha))
  if (q.isPending) return <PageSkeleton rows={4} />
  if (q.isError) return <ErrorState error={q.error} />
  const { commit: c, diff } = q.data
  return (
    <Container width="wide" className="flex flex-col gap-5">
      <div className="overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
        <div className="flex items-start gap-3 px-5 py-4">
          <GitCommit size={18} className="mt-0.5 shrink-0 text-fg-faint" />
          <div className="min-w-0 flex-1">
            <h1 className="text-xl font-semibold tracking-tight">{c.subject}</h1>
            {c.body && <pre className="mt-2 font-sans text-base whitespace-pre-wrap text-fg-muted">{c.body}</pre>}
          </div>
          <ButtonLink to="/$repo" params={{ repo: name }} search={{ ref: c.sha }} icon={<FolderTree size={14} />}>
            Browse files
          </ButtonLink>
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-border bg-bg-subtle px-5 py-2.5 text-sm text-fg-muted">
          <span className="flex items-center gap-2">
            <Avatar user={commitAuthor(c)} size={20} />
            <span>
              <span className="font-medium text-fg">{c.author.name}</span> authored <Time iso={c.author.date} />
              {c.committer.name !== c.author.name && (
                <>
                  {' '}
                  · <span className="font-medium text-fg">{c.committer.name}</span> committed <Time iso={c.committer.date} />
                </>
              )}
            </span>
          </span>
          <div className="flex-1" />
          {c.parents.length > 0 && (
            <span>
              {c.parents.length === 1 ? 'Parent ' : 'Parents '}
              {c.parents.map((p) => (
                <Link key={p} to="/$repo/commit/$sha" params={{ repo: name, sha: p }} className="mr-1 font-mono text-accent hover:underline">
                  {shortSha(p)}
                </Link>
              ))}
            </span>
          )}
          <span className="flex items-center gap-1">
            Commit <code className="font-mono text-fg">{c.sha.slice(0, 12)}</code>
            <CopyButton value={c.sha} label="Copy full SHA" size="xs" />
          </span>
        </div>
      </div>
      <DiffView diff={diff} />
    </Container>
  )
}
