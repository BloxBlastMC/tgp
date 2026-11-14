// Ranking for the command palette: the best match comes first, so typing a
// page's name and pressing Enter opens that page.
type Rankable = { label: string; group: string }

function rank(c: Rankable, q: string): number {
  const label = c.label.toLowerCase()
  // "Admin: CI settings" is matched on the part after the group prefix too.
  const bare = label.includes(': ') ? label.slice(label.indexOf(': ') + 2) : label
  if (label === q || bare === q) return 0
  if (label.startsWith(q) || bare.startsWith(q)) return 1
  if (label.split(/[\s:/]+/).some((w) => w.startsWith(q))) return 2
  if (label.includes(q)) return 3
  if (c.group.toLowerCase().includes(q)) return 4
  return -1
}

export function filterCommands<T extends Rankable>(commands: T[], query: string): T[] {
  const q = query.trim().toLowerCase()
  if (!q) return commands
  return commands
    .map((c, i) => ({ c, i, r: rank(c, q) }))
    .filter((x) => x.r >= 0)
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .map((x) => x.c)
}
