// Recently visited repositories, kept per browser in localStorage.

const KEY = 'tgp-recent-repos'
const MAX = 8

export function mergeVisit(list: string[], name: string, max = MAX): string[] {
  return [name, ...list.filter((n) => n.toLowerCase() !== name.toLowerCase())].slice(0, max)
}

/**
 * The repositories to show as "recent": visited ones that still exist (in
 * visit order), topped up with the most recently pushed.
 */
export function pickRecent<R extends { name: string; pushed_at?: string | null }>(visited: string[], repos: R[], n: number): R[] {
  const byName = new Map(repos.map((r) => [r.name.toLowerCase(), r]))
  const out: R[] = []
  for (const v of visited) {
    const r = byName.get(v.toLowerCase())
    if (r && !out.includes(r)) out.push(r)
    if (out.length >= n) return out
  }
  const pushed = repos.filter((r) => !out.includes(r) && r.pushed_at).sort((a, b) => (b.pushed_at ?? '').localeCompare(a.pushed_at ?? ''))
  return [...out, ...pushed].slice(0, n)
}

export function loadVisits(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '[]')
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
  } catch {
    return []
  }
}

export function recordVisit(name: string): string[] {
  const next = mergeVisit(loadVisits(), name)
  try {
    localStorage.setItem(KEY, JSON.stringify(next))
  } catch {
    // storage unavailable; the list just won't persist
  }
  window.dispatchEvent(new Event('tgp-recent'))
  return next
}
