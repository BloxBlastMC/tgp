import { Link } from '@tanstack/react-router'
import { FolderTree } from 'lucide-react'
import type { Commit } from '../../api/client'
import { dayKey, formatDate, shortSha } from '../../lib/format'
import { Avatar, CopyButton, IconLink, Time } from '../ui'

export function commitAuthor(c: Commit) {
  return { display_name: c.author.name, username: c.author.email || c.author.name }
}

/** One commit: author avatar, subject, sha with copy, browse-at link. */
export function CommitRow({ repo, c, browse = true }: { repo: string; c: Commit; browse?: boolean }) {
  return (
    <li className="group relative flex items-center gap-3 border-b border-border px-4 py-2.5 transition-colors last:border-0 hover:bg-bg-hover">
      <Avatar user={commitAuthor(c)} size={22} />
      <div className="min-w-0 flex-1">
        <Link to="/$repo/commit/$sha" params={{ repo, sha: c.sha }} className="block truncate font-medium hover:text-accent">
          {c.subject}
        </Link>
        <div className="truncate text-sm text-fg-muted">
          <span className="text-fg">{c.author.name}</span> committed <Time iso={c.committer.date} />
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-0.5">
        <Link
          to="/$repo/commit/$sha"
          params={{ repo, sha: c.sha }}
          className="rounded-md border border-border bg-bg-subtle px-1.5 py-0.5 font-mono text-sm text-fg-muted transition-colors hover:border-border-strong hover:text-accent"
        >
          {shortSha(c.sha)}
        </Link>
        <CopyButton value={c.sha} label="Copy full SHA" />
        {browse && (
          <IconLink to="/$repo" params={{ repo }} search={{ ref: c.sha }} label="Browse files at this commit" size="sm">
            <FolderTree size={13} />
          </IconLink>
        )}
      </div>
    </li>
  )
}

function groupByDay(commits: Commit[]) {
  const groups: { day: string; commits: Commit[] }[] = []
  for (const c of commits) {
    const day = dayKey(c.committer.date)
    const last = groups[groups.length - 1]
    if (last?.day === day) last.commits.push(c)
    else groups.push({ day, commits: [c] })
  }
  return groups
}

/** Commits grouped by day, each group a card. */
export function CommitList({ repo, commits, browse = true }: { repo: string; commits: Commit[]; browse?: boolean }) {
  return (
    <div className="flex flex-col gap-5">
      {groupByDay(commits).map((g) => (
        <section key={g.day}>
          <h3 className="mb-2 flex items-center gap-2 text-sm font-medium text-fg-muted">
            <span className="size-1.5 rounded-full bg-border-strong" aria-hidden />
            Commits on {formatDate(g.day)}
          </h3>
          <ul className="overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
            {g.commits.map((c) => (
              <CommitRow key={c.sha} repo={repo} c={c} browse={browse} />
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}
