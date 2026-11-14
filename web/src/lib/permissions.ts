// Repository permission checks. Permissions are ordered read < triage < write < admin.

export type Permission = 'read' | 'triage' | 'write' | 'admin'

const order: Record<string, number> = { read: 0, triage: 1, write: 2, admin: 3 }

export function atLeast(p: string | undefined | null, min: Permission): boolean {
  return p != null && (order[p] ?? -1) >= order[min]!
}

/** Open issues, comment, label, assign. */
export const canTriage = (p: string | undefined | null) => atLeast(p, 'triage')
/** Push, merge, manage releases, CI and webhooks. */
export const canWrite = (p: string | undefined | null) => atLeast(p, 'write')
/** Repository settings that need admin (delete, access). */
export const isRepoAdmin = (p: string | undefined | null) => atLeast(p, 'admin')
