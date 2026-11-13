import type { PullEvent } from '../api/client'

/**
 * Black or white text for a #rrggbb label background, by perceived
 * brightness (the convention label UIs use, e.g. white on bug-red d73a4a).
 */
export function labelTextColor(hex: string): '#000000' | '#ffffff' {
  const h = hex.replace(/^#/, '')
  if (!/^[0-9a-fA-F]{6}$/.test(h)) return '#ffffff'
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16))
  const brightness = (r! * 0.299 + g! * 0.587 + b! * 0.114) / 255
  return brightness > 0.6 ? '#000000' : '#ffffff'
}

/** A random pleasant label colour (6 hex digits, no #). */
export function randomLabelColor(rand: () => number = Math.random): string {
  const hue = Math.floor(rand() * 360)
  const s = 0.55 + rand() * 0.3
  const l = 0.45 + rand() * 0.2
  // HSL → RGB
  const k = (n: number) => (n + hue / 30) % 12
  const a = s * Math.min(l, 1 - l)
  const f = (n: number) => l - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)))
  return [f(0), f(8), f(4)].map((x) => Math.round(x * 255).toString(16).padStart(2, '0')).join('')
}

export function isHexColor(s: string): boolean {
  return /^#?[0-9a-fA-F]{6}$/.test(s.trim())
}

export type IssueState = 'open' | 'closed'
export type IssueSort = 'updated' | 'created' | 'comments'

/** Issue list filters, kept in the URL search params. */
export type IssueListSearch = {
  state?: IssueState
  labels?: string // comma-separated
  milestone?: string // title, "none" or "*"
  assignee?: string // username, "none" or "*"
  author?: string
  q?: string
  sort?: IssueSort
}

const str = (v: unknown) => (typeof v === 'string' && v.trim() !== '' ? v : undefined)

export function validateIssueList(s: Record<string, unknown>): IssueListSearch {
  const out: IssueListSearch = {}
  if (s.state === 'closed') out.state = 'closed'
  if (s.sort === 'created' || s.sort === 'comments') out.sort = s.sort
  for (const k of ['labels', 'milestone', 'assignee', 'author', 'q'] as const) {
    const v = str(s[k])
    if (v !== undefined) out[k] = v
  }
  return out
}

/** Label names from the comma-separated search value. */
export function splitLabels(v?: string): string[] {
  return (v ?? '')
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)
}

/** Toggles a label in the comma-separated search value (undefined when empty). */
export function toggleLabel(v: string | undefined, name: string): string | undefined {
  const cur = splitLabels(v)
  const has = cur.some((x) => x.toLowerCase() === name.toLowerCase())
  const next = has ? cur.filter((x) => x.toLowerCase() !== name.toLowerCase()) : [...cur, name]
  return next.length ? next.join(',') : undefined
}

/** True when any filter other than state/sort is active. */
export function hasFilters(s: IssueListSearch): boolean {
  return !!(s.labels || s.milestone || s.assignee || s.author || s.q)
}

/** Text for events that only occur on issues or need issue wording. Returns null for others. */
export function issueEventText(e: PullEvent): string | null {
  const d = e.data as Record<string, unknown>
  const s = (k: string) => String(d[k] ?? '')
  switch (e.type) {
    case 'labeled':
      return 'added the label'
    case 'unlabeled':
      return 'removed the label'
    case 'milestoned':
      return `added this to the ${s('milestone')} milestone`
    case 'demilestoned':
      return `removed this from the ${s('milestone')} milestone`
    case 'assigned':
      return `assigned ${s('assignee')}`
    case 'unassigned':
      return `unassigned ${s('assignee')}`
    case 'closed':
      if (d.pull != null) return 'closed this in'
      if (d.commit) return 'closed this in commit'
      if (d.reason === 'not_planned') return 'closed this as not planned'
      if (d.reason === 'completed') return 'closed this as completed'
      return 'closed this'
  }
  return null
}

/** Progress of a milestone, 0–100. */
export function milestoneProgress(m: { open_issues: number; closed_issues: number }): number {
  const total = m.open_issues + m.closed_issues
  return total === 0 ? 0 : Math.round((m.closed_issues / total) * 100)
}

/** A milestone due date (YYYY-MM-DD) is in the past. */
export function isOverdue(due: string | null | undefined, state: string, now = new Date()): boolean {
  if (!due || state !== 'open') return false
  const today = now.toISOString().slice(0, 10)
  return due < today
}
