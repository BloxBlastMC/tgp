// Display state for pull requests and issues: one place for labels and tones.

export type StateTone = 'success' | 'done' | 'danger' | 'neutral'

export type PullStateKey = 'open' | 'draft' | 'merged' | 'closed'

export function pullState(pr: { state: string; draft?: boolean }): { key: PullStateKey; label: string; tone: StateTone } {
  if (pr.state === 'merged') return { key: 'merged', label: 'Merged', tone: 'done' }
  if (pr.state === 'closed') return { key: 'closed', label: 'Closed', tone: 'danger' }
  if (pr.draft) return { key: 'draft', label: 'Draft', tone: 'neutral' }
  return { key: 'open', label: 'Open', tone: 'success' }
}

export type IssueStateKey = 'open' | 'completed' | 'not_planned'

export function issueState(issue: { state: string; state_reason?: string | null }): { key: IssueStateKey; label: string; tone: StateTone } {
  if (issue.state === 'open') return { key: 'open', label: 'Open', tone: 'success' }
  if (issue.state_reason === 'not_planned') return { key: 'not_planned', label: 'Closed as not planned', tone: 'neutral' }
  return { key: 'completed', label: 'Closed', tone: 'done' }
}
