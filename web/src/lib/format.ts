// Formatting helpers shared across pages. Pure, so they're unit tested.

/** 512 B, 4 KiB, 12 MiB, 1.5 GiB. */
export function formatBytes(n: number): string {
  if (n >= 1 << 30) return `${+(n / 2 ** 30).toFixed(1)} GiB`
  if (n >= 1 << 20) return `${+(n / 2 ** 20).toFixed(n >= 100 * 2 ** 20 ? 0 : 1)} MiB`
  if (n >= 1 << 10) return `${+(n / 1024).toFixed(n >= 100 * 1024 ? 0 : 1)} KiB`
  return `${n} B`
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

/** "3 minutes ago", "in 2 days", "just now" or "never". */
export function relativeTime(iso?: string | null, now = Date.now()): string {
  if (!iso) return 'never'
  const diff = (new Date(iso).getTime() - now) / 1000
  const abs = Math.abs(diff)
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 31536000],
    ['month', 2592000],
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ]
  for (const [unit, secs] of units) {
    if (abs >= secs) return rtf.format(Math.round(diff / secs), unit)
  }
  return 'just now'
}

/** "Nov 14, 2026" for an ISO timestamp or a YYYY-MM-DD date. */
export function formatDate(iso: string, opts: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric', year: 'numeric' }): string {
  // A bare date is a calendar day, not midnight UTC.
  const d = /^\d{4}-\d{2}-\d{2}$/.test(iso) ? new Date(`${iso}T00:00:00`) : new Date(iso)
  return d.toLocaleDateString(undefined, opts)
}

/** Local calendar day (YYYY-MM-DD) of a timestamp. */
export function dayKey(iso: string): string {
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

function daysBetween(a: string, b: string): number {
  const t = (s: string) => Date.UTC(+s.slice(0, 4), +s.slice(5, 7) - 1, +s.slice(8, 10))
  return Math.round((t(b) - t(a)) / 86400000)
}

/**
 * Due-date wording for a milestone due on `due` (YYYY-MM-DD).
 * Returns the text and whether it's overdue.
 */
export function dueLabel(due: string | null | undefined, state: string, now = new Date()): { text: string; overdue: boolean } | null {
  if (!due) return null
  if (state !== 'open') return { text: `Due ${formatDate(due)}`, overdue: false }
  const today = dayKey(now.toISOString())
  const days = daysBetween(today, due)
  const plural = (n: number) => `${n} day${n === 1 ? '' : 's'}`
  if (days < 0) return { text: `Overdue by ${plural(-days)}`, overdue: true }
  if (days === 0) return { text: 'Due today', overdue: false }
  if (days === 1) return { text: 'Due tomorrow', overdue: false }
  if (days <= 14) return { text: `Due in ${plural(days)}`, overdue: false }
  return { text: `Due ${formatDate(due)}`, overdue: false }
}

/** "Good morning" / "Good afternoon" / "Good evening". */
export function greeting(hour: number): string {
  if (hour < 5) return 'Good evening'
  if (hour < 12) return 'Good morning'
  if (hour < 18) return 'Good afternoon'
  return 'Good evening'
}

/** "1 file", "3 files". */
export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}

/** Compact counts: 999, 1.2k, 12k. */
export function compactNumber(n: number): string {
  if (n < 1000) return String(n)
  if (n < 10000) return `${+(n / 1000).toFixed(1)}k`
  return `${Math.round(n / 1000)}k`
}

export function shortSha(sha: string): string {
  return sha.slice(0, 7)
}
