// Matching for searchable lists (pickers, comboboxes): prefix matches first,
// then word-start matches, then substring matches; ties keep input order.

export function matchRank(text: string, q: string): number {
  const t = text.toLowerCase()
  if (t === q) return 0
  if (t.startsWith(q)) return 1
  if (t.split(/[\s/._-]+/).some((w) => w.startsWith(q))) return 2
  if (t.includes(q)) return 3
  return -1
}

export function filterItems<T>(items: T[], query: string, text: (item: T) => string): T[] {
  const q = query.trim().toLowerCase()
  if (!q) return items
  return items
    .map((item, i) => ({ item, i, r: matchRank(text(item), q) }))
    .filter((x) => x.r >= 0)
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .map((x) => x.item)
}
