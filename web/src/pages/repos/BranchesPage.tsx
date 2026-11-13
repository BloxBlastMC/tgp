import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { GitBranch, Tag } from 'lucide-react'
import type { Schemas } from '../../api/client'
import { branchesQuery, repoQuery, tagsQuery } from '../../api/queries'
import { Badge, Spinner, Time } from '../../components/ui'
import { useRepoName } from './RepoLayout'

function RefList({ title, icon, refs, repo }: { title: string; icon: React.ReactNode; refs: Schemas['Ref'][]; repo: string }) {
  return (
    <section>
      <h3 className="mb-1.5 flex items-center gap-1.5 text-[12px] font-medium text-fg-muted">
        {icon} {title} <span className="text-fg-faint">{refs.length}</span>
      </h3>
      {refs.length === 0 ? (
        <p className="rounded-md border border-border px-3 py-4 text-center text-fg-faint">None yet.</p>
      ) : (
        <ul className="overflow-hidden rounded-md border border-border">
          {refs.map((r) => (
            <li key={r.name} className="flex items-center gap-3 border-b border-border px-3 py-2 last:border-0 hover:bg-bg-subtle">
              <Link to="/$repo" params={{ repo }} search={{ ref: r.name }} className="font-mono text-[13px] font-medium hover:text-accent">
                {r.name}
              </Link>
              {r.is_default && <Badge tone="accent">default</Badge>}
              <Link to="/$repo/commit/$sha" params={{ repo, sha: r.commit }} className="min-w-0 flex-1 truncate text-fg-muted hover:text-accent">
                {r.subject}
              </Link>
              <span className="shrink-0 text-[12px] text-fg-faint">
                {r.author} · <Time iso={r.date} />
              </span>
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
  if (branches.isPending || tags.isPending) return <Spinner />
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-5 p-4">
      <RefList title="Branches" icon={<GitBranch size={13} />} refs={branches.data ?? []} repo={repo.name} />
      <RefList title="Tags" icon={<Tag size={13} />} refs={tags.data ?? []} repo={repo.name} />
    </div>
  )
}
