import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowDown, ArrowUp, RefreshCw } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { errorMessage, type Schemas } from '../../api/client'
import { healthQuery } from '../../api/queries'
import { Alert, Badge, Button, cx, Spinner, Time } from '../../components/ui'
import { formatBytes } from '../../lib/ci'
import { age, diskTone, formatUptime, sortChecks, sortRepos, usedPercent, type HealthStatus, type RepoSortKey } from '../../lib/health'

type Health = Schemas['ServerHealth']

const dot: Record<HealthStatus, string> = { ok: 'bg-success', warn: 'bg-warning', fail: 'bg-danger' }
const bar: Record<HealthStatus, string> = { ok: 'bg-accent', warn: 'bg-warning', fail: 'bg-danger' }
const badge: Record<HealthStatus, 'success' | 'warning' | 'danger'> = { ok: 'success', warn: 'warning', fail: 'danger' }
const banner: Record<HealthStatus, { cls: string; text: string }> = {
  ok: { cls: 'border-success/30 bg-success/10 text-success', text: 'All systems healthy' },
  warn: { cls: 'border-warning/30 bg-warning/10 text-warning', text: 'Needs attention' },
  fail: { cls: 'border-danger/30 bg-danger/10 text-danger', text: 'Something is broken' },
}

export function AdminHealthPage() {
  const q = useQuery(healthQuery())
  const qc = useQueryClient()
  const remeasure = useMutation({
    mutationFn: () => qc.fetchQuery({ ...healthQuery(true), staleTime: 0 }),
    onSuccess: (data) => qc.setQueryData(healthQuery().queryKey, data),
  })

  if (q.isPending) return <Spinner />
  if (q.isError)
    return (
      <div className="p-4">
        <Alert>{errorMessage(q.error)}</Alert>
      </div>
    )
  const h = q.data
  return (
    <div className="flex flex-col gap-4 p-4">
      <StatusBanner h={h} refreshing={q.isFetching} onRefresh={() => void q.refetch()} />
      {remeasure.error && <Alert>{errorMessage(remeasure.error)}</Alert>}
      <Checks checks={h.checks} />
      <Tiles h={h} />
      <div className="grid gap-4 xl:grid-cols-2">
        <Disks disks={h.disks} />
        <Storage s={h.storage} />
      </div>
      <Repos repos={h.repos} measuring={remeasure.isPending} onMeasure={() => remeasure.mutate()} />
    </div>
  )
}

function StatusBanner({ h, refreshing, onRefresh }: { h: Health; refreshing: boolean; onRefresh: () => void }) {
  const b = banner[h.status]
  return (
    <div role="status" aria-label="Overall status" data-status={h.status} className={cx('flex items-center gap-3 rounded-md border px-4 py-3', b.cls)}>
      <span className={cx('size-2.5 shrink-0 rounded-full', dot[h.status])} />
      <div className="flex-1">
        <div className="font-medium">{b.text}</div>
        <div className="text-sm text-fg-muted">
          tgp {h.version} <span className="font-mono">{h.commit}</span> · up {formatUptime(h.uptime_seconds)} · checked <Time iso={h.generated_at} /> · refreshes every 30s
        </div>
      </div>
      <Button variant="ghost" loading={refreshing} onClick={onRefresh} aria-label="Refresh">
        <RefreshCw size={13} /> Refresh
      </Button>
    </div>
  )
}

function Checks({ checks }: { checks: Health['checks'] }) {
  return (
    <section className="rounded-md border border-border bg-bg-subtle">
      <h2 className="border-b border-border px-4 py-2 text-xs font-medium tracking-wide text-fg-faint uppercase">Checks</h2>
      <ul aria-label="Health checks">
        {sortChecks(checks).map((c) => (
          <li key={c.name} className="flex items-center gap-3 border-b border-border px-4 py-1.5 last:border-b-0" data-testid="health-check">
            <span className={cx('size-2 shrink-0 rounded-full', dot[c.status])} title={c.status} />
            <span className="w-32 shrink-0 font-mono text-sm">{c.name}</span>
            <span className="flex-1 text-base text-fg-muted">{c.message}</span>
            {c.status !== 'ok' && <Badge tone={badge[c.status]}>{c.status}</Badge>}
          </li>
        ))}
      </ul>
    </section>
  )
}

function Tile({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="rounded-md border border-border bg-bg-subtle px-4 py-3">
      <h2 className="mb-2 text-xs font-medium tracking-wide text-fg-faint uppercase">{title}</h2>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">{children}</dl>
    </section>
  )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-fg-muted">{label}</dt>
      <dd className="truncate text-right tabular-nums">{children}</dd>
    </>
  )
}

function Tiles({ h }: { h: Health }) {
  const { database: db, ci, webhooks: wh, counts: n, host } = h
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-5">
      <Tile title="Database">
        <Row label="Driver">
          <span className="inline-flex items-center gap-1.5">
            <span className={cx('size-1.5 rounded-full', db.ok ? 'bg-success' : 'bg-danger')} />
            {db.driver}
          </span>
        </Row>
        <Row label="Latency">{db.latency_ms.toFixed(1)} ms</Row>
        <Row label="Size">{db.size_bytes == null ? '—' : formatBytes(db.size_bytes)}</Row>
        <Row label="Schema">
          <span className="font-mono text-xs" title={db.schema_version}>
            {db.schema_version.slice(0, 14) || '—'}
          </span>
        </Row>
        <Row label="Pending migrations">{db.pending_migrations}</Row>
        {db.error && <dd className="col-span-2 text-danger">{db.error}</dd>}
      </Tile>
      <Tile title="CI">
        <Row label="Queued">{ci.queued_jobs}</Row>
        <Row label="Running">{ci.running_jobs}</Row>
        <Row label="Oldest queued">{age(ci.oldest_queued_at)}</Row>
        <Row label="Runners">
          {ci.runners_online} on · {ci.runners_offline} off · {ci.runners_disabled} disabled
        </Row>
        <Row label="Slots">
          {ci.slots_busy} / {ci.slots_total} busy
        </Row>
      </Tile>
      <Tile title="Webhooks">
        <Row label="Hooks">
          {wh.active_hooks} active of {wh.hooks}
        </Row>
        <Row label="Pending">{wh.pending_deliveries}</Row>
        <Row label="Retrying">{wh.retrying_deliveries}</Row>
        <Row label="Failed (24h)">{wh.failed_deliveries_24h}</Row>
        <Row label="Oldest pending">{age(wh.oldest_pending_at)}</Row>
      </Tile>
      <Tile title="Usage">
        <Row label="Users">
          {n.users}
          {n.disabled_users > 0 && <span className="text-fg-faint"> ({n.disabled_users} disabled)</span>}
        </Row>
        <Row label="Sessions">{n.sessions}</Row>
        <Row label="Repositories">{n.repos}</Row>
        <Row label="Open PRs">{n.open_pulls}</Row>
        <Row label="Open issues">{n.open_issues}</Row>
      </Tile>
      <Tile title="Host">
        <Row label="Hostname">{host.hostname}</Row>
        <Row label="Platform">
          {host.os}/{host.arch}
        </Row>
        <Row label="CPUs">{host.cpus}</Row>
        <Row label="Goroutines">{host.goroutines}</Row>
        <Row label="Memory">{formatBytes(host.memory_bytes)}</Row>
        <Row label="Started">
          <Time iso={h.started_at} />
        </Row>
      </Tile>
    </div>
  )
}

function Disks({ disks }: { disks: Health['disks'] }) {
  return (
    <section aria-label="Disks" className="rounded-md border border-border bg-bg-subtle">
      <h2 className="border-b border-border px-4 py-2 text-xs font-medium tracking-wide text-fg-faint uppercase">Disks</h2>
      {disks.length === 0 && <p className="px-4 py-3 text-fg-muted">No local filesystems to report.</p>}
      {disks.map((d) => {
        const pct = usedPercent(d)
        const tone = diskTone(pct)
        return (
          <div key={d.path} className="border-b border-border px-4 py-2.5 last:border-b-0" data-testid="disk-row">
            <div className="flex items-center gap-2">
              <span className="truncate font-mono text-sm">{d.path}</span>
              {d.uses.map((u) => (
                <span key={u} className="rounded border border-border px-1 text-xs text-fg-muted">
                  {u}
                </span>
              ))}
              <span className="flex-1" />
              <span className={cx('text-sm tabular-nums', tone === 'ok' ? 'text-fg-muted' : tone === 'warn' ? 'text-warning' : 'text-danger')}>
                {formatBytes(d.free_bytes)} free
              </span>
            </div>
            <div className="mt-1.5 flex items-center gap-2">
              <div className="h-1.5 flex-1 overflow-hidden rounded bg-bg-hover" role="meter" aria-label={`${d.path} used`} aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100}>
                <div className={cx('h-full', bar[tone])} style={{ width: `${pct}%` }} />
              </div>
              <span className="w-40 text-right text-xs text-fg-faint tabular-nums">
                {formatBytes(d.used_bytes)} / {formatBytes(d.total_bytes)} ({pct.toFixed(0)}%)
              </span>
            </div>
          </div>
        )
      })}
    </section>
  )
}

const parts = [
  { key: 'logs_bytes', count: null, label: 'CI logs', color: 'bg-info' },
  { key: 'artifacts_bytes', count: 'artifacts_count', label: 'Artifacts', color: 'bg-accent' },
  { key: 'caches_bytes', count: 'caches_count', label: 'Caches', color: 'bg-warning' },
  { key: 'release_assets_bytes', count: 'release_assets_count', label: 'Release assets', color: 'bg-success' },
] as const

function Storage({ s }: { s: Health['storage'] }) {
  const total = Math.max(1, s.total_bytes)
  return (
    <section aria-label="Blob storage" className="rounded-md border border-border bg-bg-subtle">
      <h2 className="flex items-center gap-2 border-b border-border px-4 py-2 text-xs font-medium tracking-wide text-fg-faint uppercase">
        Blob storage
        <span className="rounded border border-border px-1 font-mono normal-case">{s.type}</span>
        <span className="flex-1" />
        <span className={cx('normal-case', s.ok ? 'text-success' : 'text-danger')}>{s.ok ? 'reachable' : 'unreachable'}</span>
      </h2>
      <div className="px-4 py-3">
        {s.error && (
          <div className="mb-2">
            <Alert>{s.error}</Alert>
          </div>
        )}
        <div className="flex items-baseline justify-between text-sm">
          <span className="text-fg-muted">Total</span>
          <span className="font-medium tabular-nums">{formatBytes(s.total_bytes)}</span>
        </div>
        <div className="mt-1.5 flex h-1.5 overflow-hidden rounded bg-bg-hover" aria-hidden>
          {parts.map((p) => (
            <div key={p.key} className={p.color} style={{ width: `${(s[p.key] / total) * 100}%` }} />
          ))}
        </div>
        <dl className="mt-3 grid grid-cols-[auto_1fr_auto] gap-x-4 gap-y-1 text-sm">
          {parts.map((p) => (
            <div key={p.key} className="contents">
              <dt className="flex items-center gap-2 text-fg-muted">
                <span className={cx('size-2 rounded-sm', p.color)} />
                {p.label}
              </dt>
              <dd className="text-right text-fg-faint tabular-nums">{p.count ? `${s[p.count]} files` : ''}</dd>
              <dd className="text-right tabular-nums">{formatBytes(s[p.key])}</dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  )
}

const columns: { key: RepoSortKey; label: string }[] = [
  { key: 'name', label: 'Repository' },
  { key: 'git_bytes', label: 'Git' },
  { key: 'logs_bytes', label: 'Logs' },
  { key: 'artifacts_bytes', label: 'Artifacts' },
  { key: 'caches_bytes', label: 'Caches' },
  { key: 'release_assets_bytes', label: 'Releases' },
  { key: 'total_bytes', label: 'Total' },
  { key: 'pushed_at', label: 'Last push' },
]

function Repos({ repos, measuring, onMeasure }: { repos: Health['repos']; measuring: boolean; onMeasure: () => void }) {
  const [sort, setSort] = useState<{ key: RepoSortKey; desc: boolean }>({ key: 'total_bytes', desc: true })
  const rows = sortRepos(repos.items, sort.key, sort.desc)
  const by = (key: RepoSortKey) => setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: key !== 'name' }))
  return (
    <section aria-label="Repository usage" className="rounded-md border border-border bg-bg-subtle">
      <header className="flex items-center gap-3 border-b border-border px-4 py-2">
        <h2 className="text-xs font-medium tracking-wide text-fg-faint uppercase">Repository usage</h2>
        <span className="text-sm text-fg-muted">
          {repos.count} repositories · {formatBytes(repos.git_bytes)} of git data · sizes measured <Time iso={repos.measured_at} />
        </span>
        <span className="flex-1" />
        <Button variant="ghost" loading={measuring} onClick={onMeasure}>
          <RefreshCw size={13} /> Refresh sizes
        </Button>
      </header>
      {rows.length === 0 ? (
        <p className="px-4 py-3 text-fg-muted">No repositories yet.</p>
      ) : (
        <table className="w-full text-left text-sm" aria-label="Repositories by storage">
          <thead className="border-b border-border text-xs tracking-wide text-fg-faint uppercase">
            <tr>
              {columns.map((c, i) => (
                <th key={c.key} className={cx('py-1.5 font-medium', i === 0 ? 'px-4' : 'px-2 text-right', i === columns.length - 1 && 'pr-4')} aria-sort={sort.key === c.key ? (sort.desc ? 'descending' : 'ascending') : undefined}>
                  <button type="button" className={cx('inline-flex items-center gap-1 uppercase hover:text-fg', sort.key === c.key && 'text-fg')} onClick={() => by(c.key)}>
                    {c.label}
                    {sort.key === c.key && (sort.desc ? <ArrowDown size={11} /> : <ArrowUp size={11} />)}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.name} className="border-b border-border last:border-b-0 hover:bg-bg-hover" data-testid="repo-usage-row">
                <td className="px-4 py-1.5">
                  <Link to="/$repo" params={{ repo: r.name }} className="font-medium hover:text-accent">
                    {r.name}
                  </Link>
                  {r.archived && (
                    <span className="ml-2">
                      <Badge>archived</Badge>
                    </span>
                  )}
                </td>
                {(['git_bytes', 'logs_bytes', 'artifacts_bytes', 'caches_bytes', 'release_assets_bytes'] as const).map((k) => (
                  <td key={k} className={cx('px-2 py-1.5 text-right tabular-nums', r[k] === 0 ? 'text-fg-faint' : 'text-fg-muted')}>
                    {formatBytes(r[k])}
                  </td>
                ))}
                <td className="px-2 py-1.5 text-right font-medium tabular-nums">{formatBytes(r.total_bytes)}</td>
                <td className="px-2 py-1.5 pr-4 text-right text-fg-muted">
                  <Time iso={r.pushed_at} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
