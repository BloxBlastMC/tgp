import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertTriangle, Boxes, CheckCircle2, Cpu, Database, FolderGit2, HardDrive, Hourglass, Play, RefreshCw, Server, Users, Webhook, XCircle } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { errorMessage, type Schemas } from '../../api/client'
import { healthQuery } from '../../api/queries'
import {
  Alert,
  Badge,
  Button,
  Card,
  cx,
  DataTable,
  DescriptionList,
  Dot,
  EmptyState,
  ErrorState,
  Meter,
  PageSkeleton,
  StatCard,
  Time,
  type Column,
  type SortDir,
} from '../../components/ui'
import { formatBytes } from '../../lib/format'
import { age, diskTone, formatUptime, sortChecks, sortRepos, usedPercent, type HealthStatus, type RepoSortKey } from '../../lib/health'

type Health = Schemas['ServerHealth']

const tone: Record<HealthStatus, 'success' | 'warning' | 'danger'> = { ok: 'success', warn: 'warning', fail: 'danger' }
const banner: Record<HealthStatus, { cls: string; icon: string; text: string; sub: string }> = {
  ok: { cls: 'border-success/30', icon: 'bg-success', text: 'All systems operational', sub: 'Every check passed.' },
  warn: { cls: 'border-warning/40', icon: 'bg-warning', text: 'Needs attention', sub: 'Some checks reported warnings.' },
  fail: { cls: 'border-danger/40', icon: 'bg-danger', text: 'Something is broken', sub: 'At least one check failed.' },
}

export function AdminHealthPage() {
  const q = useQuery(healthQuery())
  const qc = useQueryClient()
  const remeasure = useMutation({
    mutationFn: () => qc.fetchQuery({ ...healthQuery(true), staleTime: 0 }),
    onSuccess: (data) => qc.setQueryData(healthQuery().queryKey, data),
  })

  if (q.isPending) return <PageSkeleton rows={5} />
  if (q.isError) return <ErrorState error={q.error} />
  const h = q.data
  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-5 px-6 py-6">
      <StatusBanner h={h} refreshing={q.isFetching} onRefresh={() => void q.refetch()} />
      {remeasure.error && <Alert>{errorMessage(remeasure.error)}</Alert>}
      <Kpis h={h} />
      <Checks checks={h.checks} />
      <Tiles h={h} />
      <div className="grid gap-5 xl:grid-cols-2">
        <Disks disks={h.disks} />
        <Storage s={h.storage} />
      </div>
      <Repos repos={h.repos} measuring={remeasure.isPending} onMeasure={() => remeasure.mutate()} />
    </div>
  )
}

function StatusBanner({ h, refreshing, onRefresh }: { h: Health; refreshing: boolean; onRefresh: () => void }) {
  const b = banner[h.status]
  const Icon = h.status === 'ok' ? CheckCircle2 : h.status === 'warn' ? AlertTriangle : XCircle
  return (
    <div role="status" aria-label="Overall status" data-status={h.status} className={cx('flex flex-wrap items-center gap-4 rounded-xl border bg-surface-1 px-5 py-4 shadow-card', b.cls)}>
      <span className={cx('flex size-10 shrink-0 items-center justify-center rounded-full text-white', b.icon)}>
        <Icon size={20} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-lg font-semibold tracking-tight">{b.text}</div>
        <div className="text-sm text-fg-muted">
          {b.sub} tgp {h.version} <span className="font-mono">{h.commit}</span> · up {formatUptime(h.uptime_seconds)} · checked <Time iso={h.generated_at} /> · refreshes every 30s
        </div>
      </div>
      <Button loading={refreshing} onClick={onRefresh} aria-label="Refresh" icon={<RefreshCw size={13} />}>
        Refresh
      </Button>
    </div>
  )
}

function Kpis({ h }: { h: Health }) {
  const { ci, webhooks: wh, database: db, counts: n } = h
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
      <StatCard label="Queued jobs" value={ci.queued_jobs} icon={<Hourglass size={15} />} tone={ci.queued_jobs > 0 ? 'warning' : undefined} hint={ci.oldest_queued_at ? `oldest ${age(ci.oldest_queued_at)}` : 'nothing waiting'} />
      <StatCard label="Running jobs" value={ci.running_jobs} icon={<Play size={15} />} hint={`${ci.slots_busy} / ${ci.slots_total} slots busy`} />
      <StatCard label="Runners online" value={ci.runners_online} icon={<Cpu size={15} />} tone={ci.runners_online === 0 ? 'danger' : 'success'} hint={`${ci.runners_offline} offline`} />
      <StatCard label="Failed hooks" value={wh.failed_deliveries_24h} icon={<Webhook size={15} />} tone={wh.failed_deliveries_24h > 0 ? 'danger' : undefined} hint="last 24 hours" />
      <StatCard label="DB latency" value={`${db.latency_ms.toFixed(1)} ms`} icon={<Database size={15} />} tone={db.ok ? undefined : 'danger'} hint={db.driver} />
      <StatCard label="People" value={n.users} icon={<Users size={15} />} hint={`${n.sessions} sessions`} />
    </div>
  )
}

function Checks({ checks }: { checks: Health['checks'] }) {
  return (
    <Card title="Checks" flush>
      <ul aria-label="Health checks" className="-mr-px -mb-px grid sm:grid-cols-2 xl:grid-cols-3">
        {sortChecks(checks).map((c) => (
          <li key={c.name} className="flex items-start gap-3 px-4 py-3 shadow-[inset_-1px_-1px_0_var(--border)]" data-testid="health-check">
            <Dot tone={tone[c.status]} className="mt-1.5" label={c.status} />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="font-mono text-sm font-medium">{c.name}</span>
                {c.status !== 'ok' && <Badge tone={tone[c.status]}>{c.status}</Badge>}
              </div>
              <p className="text-sm text-fg-muted">{c.message}</p>
            </div>
          </li>
        ))}
      </ul>
    </Card>
  )
}

function Tile({ title, icon, children }: { title: string; icon: ReactNode; children: { term: ReactNode; detail: ReactNode }[] }) {
  return (
    <Card title={title} icon={icon} bodyClassName="text-sm">
      <DescriptionList items={children} termWidth="grid-cols-[auto_minmax(0,1fr)]" className="[&_dd]:text-right [&_dd]:tabular-nums" />
    </Card>
  )
}

function Tiles({ h }: { h: Health }) {
  const { database: db, ci, webhooks: wh, counts: n, host } = h
  return (
    <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-5">
      <Tile
        title="Database"
        icon={<Database size={14} />}
        children={[
          {
            term: 'Driver',
            detail: (
              <span className="inline-flex items-center gap-1.5">
                <Dot tone={db.ok ? 'success' : 'danger'} />
                {db.driver}
              </span>
            ),
          },
          { term: 'Latency', detail: `${db.latency_ms.toFixed(1)} ms` },
          { term: 'Size', detail: db.size_bytes == null ? '—' : formatBytes(db.size_bytes) },
          {
            term: 'Schema',
            detail: (
              <span className="font-mono text-xs" title={db.schema_version}>
                {db.schema_version.slice(0, 14) || '—'}
              </span>
            ),
          },
          { term: 'Pending migrations', detail: db.pending_migrations },
          ...(db.error ? [{ term: 'Error', detail: <span className="text-danger">{db.error}</span> }] : []),
        ]}
      />
      <Tile
        title="CI"
        icon={<Play size={14} />}
        children={[
          { term: 'Queued', detail: ci.queued_jobs },
          { term: 'Running', detail: ci.running_jobs },
          { term: 'Oldest queued', detail: age(ci.oldest_queued_at) },
          { term: 'Runners', detail: `${ci.runners_online} on · ${ci.runners_offline} off · ${ci.runners_disabled} disabled` },
          { term: 'Slots', detail: `${ci.slots_busy} / ${ci.slots_total} busy` },
        ]}
      />
      <Tile
        title="Webhooks"
        icon={<Webhook size={14} />}
        children={[
          { term: 'Hooks', detail: `${wh.active_hooks} active of ${wh.hooks}` },
          { term: 'Pending', detail: wh.pending_deliveries },
          { term: 'Retrying', detail: wh.retrying_deliveries },
          { term: 'Failed (24h)', detail: wh.failed_deliveries_24h },
          { term: 'Oldest pending', detail: age(wh.oldest_pending_at) },
        ]}
      />
      <Tile
        title="Usage"
        icon={<Users size={14} />}
        children={[
          {
            term: 'Users',
            detail: (
              <>
                {n.users}
                {n.disabled_users > 0 && <span className="text-fg-faint"> ({n.disabled_users} disabled)</span>}
              </>
            ),
          },
          { term: 'Sessions', detail: n.sessions },
          { term: 'Repositories', detail: n.repos },
          { term: 'Open PRs', detail: n.open_pulls },
          { term: 'Open issues', detail: n.open_issues },
        ]}
      />
      <Tile
        title="Host"
        icon={<Server size={14} />}
        children={[
          { term: 'Hostname', detail: <span className="truncate">{host.hostname}</span> },
          { term: 'Platform', detail: `${host.os}/${host.arch}` },
          { term: 'CPUs', detail: host.cpus },
          { term: 'Goroutines', detail: host.goroutines },
          { term: 'Memory', detail: formatBytes(host.memory_bytes) },
          { term: 'Started', detail: <Time iso={h.started_at} /> },
        ]}
      />
    </div>
  )
}

function Disks({ disks }: { disks: Health['disks'] }) {
  return (
    <Card title="Disks" icon={<HardDrive size={14} />} flush>
      {disks.length === 0 && <p className="px-4 py-3 text-fg-muted">No local filesystems to report.</p>}
      {disks.map((d) => {
        const pct = usedPercent(d)
        const t = diskTone(pct)
        return (
          <div key={d.path} className="border-b border-border px-4 py-3 last:border-b-0" data-testid="disk-row">
            <div className="flex items-center gap-2">
              <span className="truncate font-mono text-sm font-medium">{d.path}</span>
              {d.uses.map((u) => (
                <Badge key={u}>{u}</Badge>
              ))}
              <span className="flex-1" />
              <span className={cx('text-sm tabular-nums', t === 'ok' ? 'text-fg-muted' : t === 'warn' ? 'text-warning' : 'text-danger')}>{formatBytes(d.free_bytes)} free</span>
            </div>
            <div className="mt-2 flex items-center gap-3">
              <Meter value={pct} label={`${d.path} used`} tone={t === 'ok' ? 'accent' : tone[t]} className="flex-1" />
              <span className="w-44 text-right text-xs text-fg-faint tabular-nums">
                {formatBytes(d.used_bytes)} / {formatBytes(d.total_bytes)} ({pct.toFixed(0)}%)
              </span>
            </div>
          </div>
        )
      })}
    </Card>
  )
}

const parts = [
  { key: 'logs_bytes', count: null, label: 'CI logs', color: 'bg-info' },
  { key: 'artifacts_bytes', count: 'artifacts_count', label: 'Artifacts', color: 'bg-accent-solid' },
  { key: 'caches_bytes', count: 'caches_count', label: 'Caches', color: 'bg-warning' },
  { key: 'release_assets_bytes', count: 'release_assets_count', label: 'Release assets', color: 'bg-success' },
] as const

function Storage({ s }: { s: Health['storage'] }) {
  const total = Math.max(1, s.total_bytes)
  return (
    <Card
      title="Blob storage"
      icon={<Boxes size={14} />}
      actions={
        <>
          <Badge>{s.type}</Badge>
          <Badge tone={s.ok ? 'success' : 'danger'}>{s.ok ? 'reachable' : 'unreachable'}</Badge>
        </>
      }
    >
      {s.error && (
        <div className="mb-3">
          <Alert>{s.error}</Alert>
        </div>
      )}
      <div className="flex items-baseline justify-between">
        <span className="text-sm text-fg-muted">Total</span>
        <span className="text-xl font-semibold tabular-nums">{formatBytes(s.total_bytes)}</span>
      </div>
      <div className="mt-2 flex h-2 gap-px overflow-hidden rounded-full bg-bg-active" aria-hidden>
        {parts.map((p) => (
          <div key={p.key} className={p.color} style={{ width: `${(s[p.key] / total) * 100}%` }} />
        ))}
      </div>
      <dl className="mt-4 grid grid-cols-[auto_1fr_auto] gap-x-4 gap-y-1.5 text-sm">
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
    </Card>
  )
}

const sizeCols = [
  { key: 'git_bytes', label: 'Git' },
  { key: 'logs_bytes', label: 'Logs' },
  { key: 'artifacts_bytes', label: 'Artifacts' },
  { key: 'caches_bytes', label: 'Caches' },
  { key: 'release_assets_bytes', label: 'Releases' },
] as const

type RepoUsage = Health['repos']['items'][number]

function Repos({ repos, measuring, onMeasure }: { repos: Health['repos']; measuring: boolean; onMeasure: () => void }) {
  const [sort, setSort] = useState<{ id: string; dir: SortDir }>({ id: 'total_bytes', dir: 'desc' })
  const rows = sortRepos(repos.items, sort.id as RepoSortKey, sort.dir === 'desc')
  const columns: Column<RepoUsage>[] = [
    {
      id: 'name',
      header: 'Repository',
      sortValue: () => 0,
      cell: (r) => (
        <span className="flex items-center gap-2">
          <Link to="/$repo" params={{ repo: r.name }} className="font-medium hover:text-accent">
            {r.name}
          </Link>
          {r.archived && <Badge>archived</Badge>}
        </span>
      ),
    },
    ...sizeCols.map<Column<RepoUsage>>((c) => ({
      id: c.key,
      header: c.label,
      align: 'right',
      sortValue: () => 0,
      cell: (r) => <span className={cx('tabular-nums', r[c.key] === 0 ? 'text-fg-faint' : 'text-fg-muted')}>{formatBytes(r[c.key])}</span>,
    })),
    { id: 'total_bytes', header: 'Total', align: 'right', sortValue: () => 0, cell: (r) => <span className="font-medium tabular-nums">{formatBytes(r.total_bytes)}</span> },
    { id: 'pushed_at', header: 'Last push', align: 'right', sortValue: () => 0, cell: (r) => <span className="text-fg-muted"><Time iso={r.pushed_at} /></span> },
  ]
  return (
    <Card
      title="Repository usage"
      description={
        <>
          {repos.count} repositories · {formatBytes(repos.git_bytes)} of git data · sizes measured <Time iso={repos.measured_at} />
        </>
      }
      actions={
        <Button loading={measuring} onClick={onMeasure} icon={<RefreshCw size={13} />}>
          Refresh sizes
        </Button>
      }
      flush
    >
      {rows.length === 0 ? (
        <EmptyState size="inline" icon={<FolderGit2 size={18} />} title="No repositories yet" />
      ) : (
        <DataTable
          aria-label="Repositories by storage"
          columns={columns}
          rows={rows}
          rowKey={(r) => r.name}
          rowProps={() => ({ 'data-testid': 'repo-usage-row' })}
          sort={sort}
          onSortChange={(next) => setSort(sort.id === next.id ? next : { id: next.id, dir: next.id === 'name' ? 'asc' : 'desc' })}
        />
      )}
    </Card>
  )
}
