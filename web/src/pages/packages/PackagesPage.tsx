import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Boxes, ChevronRight, Tag } from 'lucide-react'
import { packagesQuery } from '../../api/queries'
import { StorageCard } from '../../components/ci/common'
import { Container, EmptyState, SubHeader } from '../../components/Page'
import { CopyButton, ErrorState, List, SkeletonRows, StatCard, Time } from '../../components/ui'
import { formatBytes } from '../../lib/ci'
import { useRepoName } from '../repos/RepoLayout'

/** A one-line shell command with a copy button (not a secret). */
export function CommandSnippet({ command, label }: { command: string; label: string }) {
  return (
    <div className="relative w-full min-w-0">
      <pre className="overflow-x-auto rounded-lg border border-border bg-bg-subtle px-3 py-2 pr-10 text-left font-mono text-sm">{command}</pre>
      <CopyButton value={command} label={`Copy ${label}`} className="absolute top-1/2 right-1.5 -translate-y-1/2" />
    </div>
  )
}

/** The registry host for docker commands: the server's configured one, or the host in the address bar. */
export function registryHost(configured: string): string {
  return configured || window.location.host
}

export function PackagesPage() {
  const repo = useRepoName()
  const q = useQuery(packagesQuery(repo))
  const host = registryHost(q.data?.registry ?? '')
  const base = q.data ? `${host}/${q.data.namespace}/${repo.toLowerCase()}` : ''
  return (
    <Container width="wide" className="flex flex-col">
      <SubHeader
        title="Packages"
        description={
          <>
            Container images stored in this server's registry.{' '}
            <details className="mt-1 inline-block align-top text-sm [&[open]>summary]:mb-1">
              <summary className="cursor-pointer text-accent hover:underline">Who can pull and push?</summary>
              Anyone with read access to {repo} can pull. Pushing needs write access, or a CI job token from a run on the default branch.
            </details>
          </>
        }
      />
      {q.isPending ? (
        <List>
          <SkeletonRows rows={3} />
        </List>
      ) : q.isError ? (
        <ErrorState error={q.error} className="p-0" />
      ) : (
        <>
          <div className="mb-4 grid gap-3 sm:grid-cols-2">
            <StatCard label="Images" value={q.data.images.length.toLocaleString()} icon={<Boxes size={15} />} />
            <StorageCard used={q.data.usage_bytes} quota={q.data.quota_bytes} meterLabel="Registry quota used" />
          </div>
          {q.data.images.length === 0 ? (
            <EmptyState size="inline" icon={<Boxes size={18} />} title="No images yet">
              <div className="flex flex-col items-center gap-2">
                <span>Log in with a personal access token, then push to {base}/&lt;name&gt;:&lt;tag&gt;.</span>
                <div className="w-full max-w-md">
                  <CommandSnippet command={`docker login ${host}`} label="docker login command" />
                </div>
              </div>
            </EmptyState>
          ) : (
            <List aria-label="Images">
              {q.data.images.map((p) => (
                <li key={p.name} className="border-b border-border last:border-0">
                  <Link
                    to="/$repo/packages/image"
                    params={{ repo }}
                    search={{ name: p.name || undefined }}
                    className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-bg-hover"
                  >
                    <Boxes size={14} className="shrink-0 text-fg-faint" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-mono text-base">{p.name || repo.toLowerCase()}</div>
                      <div className="mt-0.5 flex flex-wrap gap-x-3 text-sm text-fg-faint">
                        <span className="truncate font-mono">
                          {host}/{p.path}
                        </span>
                        {p.latest_tag && (
                          <span className="inline-flex items-center gap-1">
                            <Tag size={11} />
                            {p.latest_tag}
                          </span>
                        )}
                        <span>
                          {p.tags} {p.tags === 1 ? 'tag' : 'tags'} · {p.versions} {p.versions === 1 ? 'version' : 'versions'}
                        </span>
                        <span>
                          updated <Time iso={p.updated_at} />
                        </span>
                      </div>
                    </div>
                    <span className="shrink-0 text-sm text-fg-muted tabular-nums">{formatBytes(p.size_bytes)}</span>
                    <ChevronRight size={14} className="shrink-0 text-fg-faint" />
                  </Link>
                </li>
              ))}
            </List>
          )}
        </>
      )}
    </Container>
  )
}
