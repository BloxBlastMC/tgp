import type { Schemas } from '../api/client'

export type HealthStatus = Schemas['HealthStatus']
export type RepoUsage = Schemas['RepoUsage']

const rank: Record<HealthStatus, number> = { ok: 0, warn: 1, fail: 2 }

/** Checks worst first, keeping the server's order within a status. */
export function sortChecks<T extends { status: HealthStatus }>(checks: T[]): T[] {
  return checks
    .map((c, i) => [c, i] as const)
    .sort(([a, i], [b, j]) => rank[b.status] - rank[a.status] || i - j)
    .map(([c]) => c)
}

/** Tone for a disk usage percentage: fail at ≥95%, warn at ≥90%. */
export function diskTone(usedPercent: number): HealthStatus {
  if (usedPercent >= 95) return 'fail'
  if (usedPercent >= 90) return 'warn'
  return 'ok'
}

/** Used share of a disk, 0–100. */
export function usedPercent(d: { total_bytes: number; used_bytes: number }): number {
  if (d.total_bytes <= 0) return 0
  return Math.max(0, Math.min(100, (d.used_bytes / d.total_bytes) * 100))
}

/** 45s, 12m, 3h 4m, 2d 5h. */
export function formatUptime(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ${m % 60}m`
  const d = Math.floor(h / 24)
  return `${d}d ${h % 24}h`
}

/** Age of a timestamp as an uptime-style string, or "—". */
export function age(iso?: string | null, now = Date.now()): string {
  if (!iso) return '—'
  return formatUptime((now - new Date(iso).getTime()) / 1000)
}

export type RepoSortKey = 'name' | 'git_bytes' | 'logs_bytes' | 'artifacts_bytes' | 'caches_bytes' | 'release_assets_bytes' | 'total_bytes' | 'pushed_at'

/** Sorts repo usage rows; numbers and dates descending by default, names ascending. */
export function sortRepos(items: RepoUsage[], key: RepoSortKey, desc: boolean): RepoUsage[] {
  const val = (r: RepoUsage): number | string => {
    if (key === 'name') return r.name.toLowerCase()
    if (key === 'pushed_at') return r.pushed_at ? new Date(r.pushed_at).getTime() : 0
    return r[key]
  }
  const out = [...items].sort((a, b) => {
    const x = val(a)
    const y = val(b)
    return x < y ? -1 : x > y ? 1 : 0
  })
  return desc ? out.reverse() : out
}
