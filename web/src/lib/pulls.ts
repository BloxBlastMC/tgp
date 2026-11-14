import type { Comment, PullEvent, PullRequest, PullSummary } from '../api/client'

/** An inline comment thread: the first comment and its replies, oldest first. */
export type Thread = { root: Comment; replies: Comment[] }

/** Groups inline comments into threads. Replies whose root is missing are dropped. */
export function groupThreads(comments: Comment[]): Thread[] {
  const threads = new Map<number, Thread>()
  for (const c of comments) {
    if (c.path && c.reply_to == null) threads.set(c.id, { root: c, replies: [] })
  }
  for (const c of comments) {
    if (c.reply_to != null) threads.get(c.reply_to)?.replies.push(c)
  }
  return [...threads.values()]
}

export function lineKey(path: string, side: 'old' | 'new', line: number): string {
  return `${side}:${line}:${path}`
}

/**
 * Threads that can be shown next to the current diff, keyed by lineKey.
 * Outdated threads (the file changed since) are left out.
 */
export function anchorThreads(threads: Thread[]): Map<string, Thread[]> {
  const out = new Map<string, Thread[]>()
  for (const t of threads) {
    const { path, line, side, outdated } = t.root
    if (!path || !line || outdated) continue
    const k = lineKey(path, side ?? 'new', line)
    const list = out.get(k)
    if (list) list.push(t)
    else out.set(k, [t])
  }
  return out
}

/** The side and line number a diff line is commented on. */
export function commentAnchor(l: { type: 'context' | 'add' | 'del'; old_line?: number; new_line?: number }): { side: 'old' | 'new'; line: number } | null {
  if (l.type === 'del') return l.old_line ? { side: 'old', line: l.old_line } : null
  return l.new_line ? { side: 'new', line: l.new_line } : null
}

/**
 * Parses the splat of /<repo>/compare/<base>...<head>. Branch names may
 * contain slashes and arrive URL-encoded (feat%2Fx). Without "...", the
 * whole value is the head.
 */
export function parseCompare(splat: string | undefined): { base?: string; head?: string } {
  if (!splat) return {}
  let s = splat
  try {
    s = decodeURIComponent(splat)
  } catch {
    /* keep raw */
  }
  const i = s.indexOf('...')
  if (i < 0) return { head: s || undefined }
  return { base: s.slice(0, i) || undefined, head: s.slice(i + 3) || undefined }
}

export function compareSplat(base: string, head: string): string {
  return `${base}...${head}`
}

/** One-line description of a timeline event (actor shown separately). */
export function eventText(e: PullEvent): string {
  const d = e.data as Record<string, unknown>
  const s = (k: string) => String(d[k] ?? '')
  const sha = (k: string) => s(k).slice(0, 7)
  switch (e.type) {
    case 'pushed':
      if (d.forced) return `force-pushed the head from ${sha('before')} to ${sha('after')}`
      return `pushed ${d.commits ?? 'new'} commit${d.commits === 1 ? '' : 's'}`
    case 'base_changed':
      return `changed the base branch from ${s('from')} to ${s('to')}`
    case 'title_changed':
      return `changed the title from “${s('from')}” to “${s('to')}”`
    case 'merged':
      return d.method === 'manual' ? `merged this by pushing ${sha('commit')} to the base branch` : `merged this (${s('method')}) as ${sha('commit')}`
    case 'closed':
      return 'closed this'
    case 'reopened':
      return 'reopened this'
    case 'ready_for_review':
      return 'marked this ready for review'
    case 'converted_to_draft':
      return 'converted this to a draft'
    case 'review_requested':
      return `requested a review from ${s('reviewer')}`
    case 'review_request_removed':
      return `removed the review request for ${s('reviewer')}`
    case 'review_dismissed':
      return `dismissed ${s('reviewer')}'s review: ${s('message')}`
    case 'head_deleted':
      return `deleted the head branch ${s('branch')}`
    case 'head_restored':
      return `restored the head branch ${s('branch')}`
    case 'branch_deleted':
      return `deleted the branch ${s('branch')}`
    case 'branch_updated':
      return `updated the branch with the base (${sha('commit')})`
  }
  return e.type
}

export type DecisionInfo = { label: string; tone: 'success' | 'danger' | 'warning' | 'neutral' }

export function decisionInfo(d: PullRequest['review_decision'] | PullSummary['review_decision']): DecisionInfo | null {
  switch (d) {
    case 'approved':
      return { label: 'Approved', tone: 'success' }
    case 'changes_requested':
      return { label: 'Changes requested', tone: 'danger' }
    case 'review_required':
      return { label: 'Review required', tone: 'warning' }
  }
  return null
}

export type MergeMethod = 'merge' | 'squash' | 'rebase'

export function loadMergeMethod(): MergeMethod {
  try {
    const v = localStorage.getItem('tgp-merge-method')
    if (v === 'merge' || v === 'squash' || v === 'rebase') return v
  } catch {
    /* ignore */
  }
  return 'merge'
}

export function saveMergeMethod(m: MergeMethod) {
  try {
    localStorage.setItem('tgp-merge-method', m)
  } catch {
    /* ignore */
  }
}

/** Default commit title/message the server would use, shown for editing. */
export function defaultMergeMessage(pr: Pick<PullRequest, 'number' | 'title' | 'head'>, method: MergeMethod): { title: string; message: string } {
  if (method === 'squash') return { title: `${pr.title} (#${pr.number})`, message: '' }
  if (method === 'merge') return { title: `Merge pull request #${pr.number} from ${pr.head.branch}`, message: pr.title }
  return { title: '', message: '' }
}

/** All remaining blockers can be bypassed by this admin. */
export function canBypass(pr: Pick<PullRequest, 'blockers'>): boolean {
  return pr.blockers.length > 0 && pr.blockers.every((b) => b.bypassable)
}
