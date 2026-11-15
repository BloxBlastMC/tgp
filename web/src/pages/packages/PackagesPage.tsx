import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Boxes, ChevronRight, HardDrive, Tag } from 'lucide-react'
import { packagesQuery } from '../../api/queries'
import { EmptyState, SubHeader } from '../../components/Page'
import { ErrorState, List, Meter, SecretValue, SkeletonRows, StatCard, Time } from '../../components/ui'
import { formatBytes } from '../../lib/ci'
import { usagePercent } from '../../lib/releases'
import { useRepoName } from '../repos/RepoLayout'

/** The registry host for docker commands: the server's configured one, or the host in the address bar. */
export function registryHost(configured: string): string {
  return configured || window.location.host
}

export function PackagesPage() {
  const repo = useRepoName()
  const q = useQuery(packagesQuery(repo))
  const host = registryHost(q.data?.registry ?? '')
  const base = q.data ? `${host}/${q.data.namespace}/${repo.toLowerCase()}` : ''
  const pct = q.data ? usagePercent(q.data.usage_bytes, q.data.quota_bytes) : null
  return (
    <div className="mx-auto flex max-w-5xl flex-col px-6 py-5">
      <SubHeader
        title="Packages"
        description={
          <>
            Container images in tgp's registry. Pull needs read access to {repo}; push needs write access, or a CI job token on a push to the default branch.
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
          <div className="mb-4 grid gap-3 sm:grid-cols-3">
            <StatCard label="Images" value={q.data.images.length.toLocaleString()} icon={<Boxes size={15} />} />
            <StatCard label="Storage used" value={formatBytes(q.data.usage_bytes)} icon={<HardDrive size={15} />} hint={q.data.quota_bytes > 0 ? `of ${formatBytes(q.data.quota_bytes)} quota` : 'No quota'} />
            <div className="flex flex-col justify-center rounded-lg border border-border bg-surface-1 px-4 py-3 shadow-card">
              <div className="mb-2 flex items-center justify-between text-sm text-fg-muted">
                <span>Quota</span>
                <span className="tabular-nums">{pct == null ? '—' : `${Math.round(pct)}%`}</span>
              </div>
              <Meter value={pct ?? 0} label="Registry quota used" />
            </div>
          </div>
          {q.data.images.length === 0 ? (
            <EmptyState size="inline" icon={<Boxes size={18} />} title="No images yet">
              <div className="flex flex-col items-center gap-2">
                <span>Log in with a personal access token, then push to {base}/&lt;name&gt;:&lt;tag&gt;.</span>
                <SecretValue value={`docker login ${host}`} label="docker login command" className="w-full max-w-md text-left" />
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
                    className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-bg-hover/60"
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
    </div>
  )
}
