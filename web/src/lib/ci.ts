import type { Schemas } from '../api/client'

type LogLine = Schemas['LogLine']

// ---- status ----

export type StatusKind = 'waiting' | 'pending' | 'queued' | 'running' | 'success' | 'failure' | 'cancelled' | 'skipped' | 'timed_out'

/** One visual status for a run, job or step from its status and conclusion. */
export function statusKind(status: string, conclusion?: string | null): StatusKind {
  if (status === 'completed') {
    switch (conclusion) {
      case 'success':
      case 'failure':
      case 'cancelled':
      case 'skipped':
      case 'timed_out':
        return conclusion
    }
    return 'failure'
  }
  if (status === 'in_progress') return 'running'
  if (status === 'waiting') return 'waiting'
  if (status === 'pending') return 'pending'
  return 'queued'
}

export const statusLabel: Record<StatusKind, string> = {
  waiting: 'Waiting',
  pending: 'Pending',
  queued: 'Queued',
  running: 'In progress',
  success: 'Success',
  failure: 'Failed',
  cancelled: 'Cancelled',
  skipped: 'Skipped',
  timed_out: 'Timed out',
}

export const statusTone: Record<StatusKind, 'success' | 'danger' | 'warning' | 'neutral' | 'accent'> = {
  waiting: 'warning',
  pending: 'neutral',
  queued: 'warning',
  running: 'accent',
  success: 'success',
  failure: 'danger',
  cancelled: 'neutral',
  skipped: 'neutral',
  timed_out: 'danger',
}

/** A job or run is finished and won't change. */
export function isDone(status: string): boolean {
  return status === 'completed'
}

/** Failed-ish conclusions that "Re-run failed jobs" re-runs. */
export function isFailed(conclusion?: string | null): boolean {
  return conclusion === 'failure' || conclusion === 'cancelled' || conclusion === 'timed_out'
}

// ---- durations ----

/** 45s, 3m 12s, 1h 4m. */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}

/** Duration between start and end (or now while running); null if not started. */
export function elapsed(start?: string | null, end?: string | null, now = Date.now()): number | null {
  if (!start) return null
  const a = new Date(start).getTime()
  const b = end ? new Date(end).getTime() : now
  return Math.max(0, b - a)
}

export function durationText(start?: string | null, end?: string | null, now = Date.now()): string {
  const ms = elapsed(start, end, now)
  return ms == null ? '' : formatDuration(ms)
}

export { formatBytes } from './format'

/** "main", "#12" or "v1.0" for a ref. */
export function refLabel(ref: string, pull?: number | null): string {
  if (pull) return `#${pull}`
  if (ref.startsWith('refs/heads/')) return ref.slice(11)
  if (ref.startsWith('refs/tags/')) return ref.slice(10)
  const m = ref.match(/^refs\/pull\/(\d+)\//)
  if (m) return `#${m[1]}`
  return ref
}

// ---- run list search ----

export type RunListSearch = {
  workflow?: string
  branch?: string
  status?: string
  event?: 'push' | 'pull_request' | 'workflow_dispatch' | 'release'
  page?: number
}

export const runStatusFilters = ['active', 'queued', 'in_progress', 'waiting', 'completed', 'success', 'failure', 'cancelled', 'skipped', 'timed_out'] as const

const str = (v: unknown) => (typeof v === 'string' && v.trim() !== '' ? v : undefined)

export function validateRunList(s: Record<string, unknown>): RunListSearch {
  const out: RunListSearch = {}
  const wf = str(s.workflow)
  if (wf) out.workflow = wf
  const br = str(s.branch)
  if (br) out.branch = br
  if (typeof s.status === 'string' && (runStatusFilters as readonly string[]).includes(s.status)) out.status = s.status
  if (s.event === 'push' || s.event === 'pull_request' || s.event === 'workflow_dispatch' || s.event === 'release') out.event = s.event
  const p = Number(s.page)
  if (Number.isInteger(p) && p > 1) out.page = p
  return out
}

export function validateRunPage(s: Record<string, unknown>): { job?: number } {
  const j = Number(s.job)
  return Number.isInteger(j) && j > 0 ? { job: j } : {}
}

// ---- ANSI ----

export type AnsiSegment = { text: string; fg?: number; bg?: number; bold?: boolean }

/**
 * Splits text with ANSI SGR escapes into styled segments. Supports the 16
 * basic colours (30–37, 90–97, 40–47, 100–107), bold (1/22) and resets
 * (0, 39, 49). Other escape sequences are dropped.
 */
export function parseAnsi(text: string): AnsiSegment[] {
  const out: AnsiSegment[] = []
  let fg: number | undefined
  let bg: number | undefined
  let bold = false
  // eslint-disable-next-line no-control-regex
  const re = /\x1b\[([0-9;?]*)([A-Za-z])|\x1b[^[]/g
  let last = 0
  const push = (t: string) => {
    if (!t) return
    const seg: AnsiSegment = { text: t }
    if (fg !== undefined) seg.fg = fg
    if (bg !== undefined) seg.bg = bg
    if (bold) seg.bold = true
    const prev = out[out.length - 1]
    if (prev && prev.fg === seg.fg && prev.bg === seg.bg && !!prev.bold === !!seg.bold) prev.text += t
    else out.push(seg)
  }
  for (let m = re.exec(text); m; m = re.exec(text)) {
    push(text.slice(last, m.index))
    last = m.index + m[0].length
    if (m[2] !== 'm') continue
    const codes = (m[1] || '0').split(';').map((c) => (c === '' ? 0 : Number(c)))
    for (let i = 0; i < codes.length; i++) {
      const c = codes[i]!
      if (c === 0) {
        fg = bg = undefined
        bold = false
      } else if (c === 1) bold = true
      else if (c === 22) bold = false
      else if (c >= 30 && c <= 37) fg = c - 30
      else if (c >= 90 && c <= 97) fg = c - 90 + 8
      else if (c === 39) fg = undefined
      else if (c >= 40 && c <= 47) bg = c - 40
      else if (c >= 100 && c <= 107) bg = c - 100 + 8
      else if (c === 49) bg = undefined
      else if (c === 38 || c === 48) {
        // 256-colour / truecolour: skip the parameters, map 0–15 when given.
        if (codes[i + 1] === 5) {
          const n = codes[i + 2]
          if (n !== undefined && n < 16) {
            if (c === 38) fg = n
            else bg = n
          }
          i += 2
        } else if (codes[i + 1] === 2) i += 4
      }
    }
  }
  push(text.slice(last))
  return out
}

/** Palette for the 16 ANSI colours, readable on dark and light backgrounds. */
export const ansiColors = [
  '#6e7681', '#f47067', '#57ab5a', '#c69026', '#539bf5', '#b083f0', '#39c5cf', '#adbac7',
  '#8b949e', '#ff938a', '#6bc46d', '#daaa3f', '#6cb6ff', '#dcbdfb', '#56d4dd', '#cdd9e5',
]

/** Strips ANSI escapes (for searching / copying). */
export function stripAnsi(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/\x1b\[[0-9;?]*[A-Za-z]|\x1b[^[]/g, '')
}

// ---- log structure ----

export type LogItem =
  | { kind: 'line'; line: LogLine; level?: 'error' | 'warning' | 'notice' | 'command' }
  | { kind: 'group'; title: string; line: LogLine; children: LogLine[] }

/**
 * Turns a step's lines into display items: `::group::title` … `::endgroup::`
 * become foldable groups (a new group closes an unclosed one), and
 * `::error::`/`::warning::`/`::notice::` lines are tagged so they can be
 * highlighted with the command prefix removed.
 */
export function buildLogItems(lines: LogLine[]): LogItem[] {
  const out: LogItem[] = []
  let group: Extract<LogItem, { kind: 'group' }> | null = null
  for (const line of lines) {
    const t = stripAnsi(line.text)
    if (t.startsWith('::group::')) {
      group = { kind: 'group', title: t.slice(9).trim() || 'Group', line, children: [] }
      out.push(group)
      continue
    }
    if (t.startsWith('::endgroup::')) {
      group = null
      continue
    }
    const item = lineItem(line, t)
    if (group) group.children.push(item.line)
    else out.push(item)
  }
  return out
}

function lineItem(line: LogLine, plain: string): Extract<LogItem, { kind: 'line' }> {
  const m = plain.match(/^::(error|warning|notice)(?:\s[^:]*)?::(.*)$/)
  if (m) return { kind: 'line', line: { ...line, text: m[2]! }, level: m[1] as 'error' | 'warning' | 'notice' }
  if (plain.startsWith('[command]')) return { kind: 'line', line: { ...line, text: plain.slice(9) }, level: 'command' }
  return { kind: 'line', line }
}

/** The level of a single line (for lines inside groups). */
export function lineLevel(line: LogLine): { text: string; level?: 'error' | 'warning' | 'notice' | 'command' } {
  const it = lineItem(line, stripAnsi(line.text))
  return { text: it.level ? it.line.text : line.text, level: it.level }
}

/** Groups lines by step number, keeping order. */
export function linesByStep(lines: LogLine[]): Map<number, LogLine[]> {
  const m = new Map<number, LogLine[]>()
  for (const l of lines) {
    let a = m.get(l.step)
    if (!a) m.set(l.step, (a = []))
    a.push(l)
  }
  return m
}

/** Appends new lines (by line number) without duplicates. */
export function appendLines(cur: LogLine[], more: LogLine[]): LogLine[] {
  const last = cur.length ? cur[cur.length - 1]!.n : 0
  const add = more.filter((l) => l.n > last)
  return add.length ? [...cur, ...add] : cur
}

// ---- jobs ----

type Job = Schemas['Job']

export type JobGroup = { key: string; jobs: Job[] }

/** Groups a run's jobs by workflow key (matrix jobs together), in order of first appearance. */
export function groupJobs(jobs: Job[]): JobGroup[] {
  const out: JobGroup[] = []
  const idx = new Map<string, JobGroup>()
  for (const j of jobs) {
    let g = idx.get(j.key)
    if (!g) {
      g = { key: j.key, jobs: [] }
      idx.set(j.key, g)
      out.push(g)
    }
    g.jobs.push(j)
  }
  return out
}

/** The overall status of a group of jobs (worst wins, running beats queued). */
export function groupKind(jobs: Job[]): StatusKind {
  const kinds = jobs.map((j) => statusKind(j.status, j.conclusion))
  const order: StatusKind[] = ['failure', 'timed_out', 'running', 'queued', 'waiting', 'pending', 'cancelled', 'success', 'skipped']
  for (const k of order) if (kinds.includes(k)) return k
  return 'pending'
}

/** The job to show when none is selected: the first failed, else running, else the first. */
export function defaultJob(jobs: Job[]): Job | undefined {
  return (
    jobs.find((j) => j.status === 'completed' && isFailed(j.conclusion)) ??
    jobs.find((j) => j.status === 'in_progress') ??
    jobs[0]
  )
}

export function stepTitle(n: number, steps: { number: number; name: string }[]): string {
  if (n === 0) return 'Set up job'
  if (n === -1) return 'Complete job'
  return steps.find((s) => s.number === n)?.name ?? `Step ${n}`
}

// ---- checks ----

/** "2 failing, 1 pending, 5 successful" style summary. */
export function checksHeadline(c: Schemas['CheckSummary']): string {
  if (c.state === 'none' && c.missing_required.length === 0) return 'No checks'
  const parts: string[] = []
  if (c.failed) parts.push(`${c.failed} failing`)
  if (c.pending) parts.push(`${c.pending} in progress`)
  if (c.missing_required.length) parts.push(`${c.missing_required.length} expected`)
  if (c.succeeded) parts.push(`${c.succeeded} successful`)
  if (c.skipped) parts.push(`${c.skipped} skipped`)
  const lead = c.state === 'failure' ? 'Some checks were not successful' : c.state === 'pending' ? 'Some checks haven’t completed yet' : 'All checks have passed'
  return `${lead} · ${parts.join(', ')}`
}


// ---- log search and export ----

/** Line numbers whose text (without ANSI escapes) contains `q`, case-insensitively. */
export function findMatches(lines: LogLine[], q: string): number[] {
  const needle = q.trim().toLowerCase()
  if (!needle) return []
  return lines.filter((l) => stripAnsi(l.text).toLowerCase().includes(needle)).map((l) => l.n)
}

/** The next match index after moving `delta` from `current`, wrapping around. */
export function stepMatch(current: number, delta: number, count: number): number {
  if (count === 0) return -1
  return (((current + delta) % count) + count) % count
}

/** Plain-text log for download: one line per entry, optional timestamps. */
export function logText(lines: LogLine[], timestamps = true): string {
  return lines.map((l) => (timestamps ? `${l.t} ${stripAnsi(l.text)}` : stripAnsi(l.text))).join('\n') + (lines.length ? '\n' : '')
}
