import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { GitBranch, GitPullRequest, Shield, Tag, Trash2 } from 'lucide-react'
import { api, errorMessage, unwrap, type Schemas } from '../../api/client'
import { branchesQuery, pullsQuery, repoQuery, tagsQuery } from '../../api/queries'
import { Alert, Badge, Spinner, Time } from '../../components/ui'
import { compareSplat } from '../../lib/pulls'
import { useRepoName } from './RepoLayout'

type RefRowExtra = (r: Schemas['Ref']) => React.ReactNode

function RefList({ title, icon, refs, repo, extra }: { title: string; icon: React.ReactNode; refs: Schemas['Ref'][]; repo: string; extra?: RefRowExtra }) {
  return (
    <section>
      <h3 className="mb-1.5 flex items-center gap-1.5 text-sm font-medium text-fg-muted">
        {icon} {title} <span className="text-fg-faint">{refs.length}</span>
      </h3>
      {refs.length === 0 ? (
        <p className="rounded-md border border-border px-3 py-4 text-center text-fg-faint">None yet.</p>
      ) : (
        <ul className="overflow-hidden rounded-md border border-border">
          {refs.map((r) => (
            <li key={r.name} className="flex items-center gap-3 border-b border-border px-3 py-2 last:border-0 hover:bg-bg-subtle">
              <Link to="/$repo" params={{ repo }} search={{ ref: r.name }} className="font-mono text-base font-medium hover:text-accent">
                {r.name}
              </Link>
              {r.is_default && <Badge tone="accent">default</Badge>}
              {r.protected && (
                <Badge tone="warning">
                  <Shield size={10} className="mr-1" /> protected
                </Badge>
              )}
              <Link to="/$repo/commit/$sha" params={{ repo, sha: r.commit }} className="min-w-0 flex-1 truncate text-fg-muted hover:text-accent">
                {r.subject}
              </Link>
              <span className="shrink-0 text-sm text-fg-faint">
                {r.author} · <Time iso={r.date} />
              </span>
              {extra?.(r)}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export function BranchesPage() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const branches = useQuery(branchesQuery(name))
  const tags = useQuery(tagsQuery(name))
  const open = useQuery(pullsQuery(name, { state: 'open' }))
  const qc = useQueryClient()
  const canWrite = (repo.permission === 'write' || repo.permission === 'admin') && !repo.archived
  const del = useMutation({
    mutationFn: (r: Schemas['Ref']) => unwrap(api.POST('/repos/{repo}/branches/delete', { params: { path: { repo: name } }, body: { name: r.name, sha: r.commit } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: branchesQuery(name).queryKey })
      void qc.invalidateQueries({ queryKey: ['repo', name, 'pull'] })
    },
  })
  if (branches.isPending || tags.isPending) return <Spinner />
  const prFor = (branch: string) => open.data?.pulls.find((p) => p.head_branch === branch)
  const extra: RefRowExtra = (r) => {
    if (r.is_default) return <span className="w-[120px] shrink-0" />
    const pr = prFor(r.name)
    return (
      <span className="flex w-[120px] shrink-0 items-center justify-end gap-1">
        {pr ? (
          <Link to="/$repo/pulls/$number" params={{ repo: name, number: String(pr.number) }} className="flex items-center gap-1 text-sm text-success hover:underline">
            <GitPullRequest size={12} /> #{pr.number}
          </Link>
        ) : (
          canWrite && (
            <Link to="/$repo/compare/$" params={{ repo: name, _splat: compareSplat(repo.default_branch, r.name) }} className="flex items-center gap-1 text-sm text-fg-muted hover:text-accent">
              <GitPullRequest size={12} /> New PR
            </Link>
          )
        )}
        {canWrite && !pr && !r.protected && (
          <button
            type="button"
            className="rounded p-1 text-fg-faint hover:bg-danger/10 hover:text-danger"
            aria-label={`Delete branch ${r.name}`}
            title="Delete branch"
            onClick={() => window.confirm(`Delete branch ${r.name}?`) && del.mutate(r)}
          >
            <Trash2 size={13} />
          </button>
        )}
      </span>
    )
  }
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-5 p-4">
      {del.isError && <Alert>{errorMessage(del.error)}</Alert>}
      <RefList title="Branches" icon={<GitBranch size={13} />} refs={branches.data ?? []} repo={repo.name} extra={extra} />
      <RefList title="Tags" icon={<Tag size={13} />} refs={tags.data ?? []} repo={repo.name} />
    </div>
  )
}
