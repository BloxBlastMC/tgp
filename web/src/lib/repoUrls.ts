/** URL of a file's raw contents at a ref. */
export function rawUrl(repo: string, ref: string, path: string) {
  return `/api/v1/repos/${encodeURIComponent(repo)}/raw?${new URLSearchParams({ ref, path })}`
}

/** "abc1234" for a 40-char sha, the ref itself otherwise. */
export function refDisplay(ref: string) {
  return /^[0-9a-f]{40}$/.test(ref) ? ref.slice(0, 7) : ref
}
