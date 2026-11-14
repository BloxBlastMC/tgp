/** Search params shared by code views: ?ref=<branch|tag|sha>&path=<path>. */
export type RefPathSearch = { ref?: string; path?: string }

export function validateRefPath(s: Record<string, unknown>): RefPathSearch {
  const out: RefPathSearch = {}
  if (typeof s.ref === 'string' && s.ref) out.ref = s.ref
  if (typeof s.path === 'string' && s.path) out.path = s.path
  return out
}

export type CommitsSearch = RefPathSearch & { page?: number }

export function validateCommits(s: Record<string, unknown>): CommitsSearch {
  const out: CommitsSearch = validateRefPath(s)
  const page = Number(s.page)
  if (Number.isInteger(page) && page > 1) out.page = page
  return out
}
