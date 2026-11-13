import { describe, expect, it } from 'vitest'
import type { PullEvent } from '../api/client'
import { hasFilters, isHexColor, isOverdue, issueEventText, labelTextColor, milestoneProgress, randomLabelColor, splitLabels, toggleLabel, validateIssueList } from './issues'

describe('labelTextColor', () => {
  it('picks readable text', () => {
    expect(labelTextColor('ffffff')).toBe('#000000')
    expect(labelTextColor('fbca04')).toBe('#000000')
    expect(labelTextColor('a2eeef')).toBe('#000000')
    expect(labelTextColor('#0075ca')).toBe('#ffffff')
    expect(labelTextColor('d73a4a')).toBe('#ffffff')
    expect(labelTextColor('000000')).toBe('#ffffff')
    expect(labelTextColor('nope')).toBe('#ffffff')
  })
})

describe('randomLabelColor', () => {
  it('returns valid hex', () => {
    for (let i = 0; i < 50; i++) expect(isHexColor(randomLabelColor())).toBe(true)
    expect(randomLabelColor(() => 0)).toMatch(/^[0-9a-f]{6}$/)
  })
})

describe('issue list search', () => {
  it('validates params', () => {
    expect(validateIssueList({ state: 'closed', sort: 'comments', labels: 'bug', q: '', milestone: 'none', x: 1 })).toEqual({
      state: 'closed',
      sort: 'comments',
      labels: 'bug',
      milestone: 'none',
    })
    expect(validateIssueList({ state: 'bogus', sort: 'updated' })).toEqual({})
  })
  it('toggles labels', () => {
    expect(toggleLabel(undefined, 'bug')).toBe('bug')
    expect(toggleLabel('bug', 'needs testing')).toBe('bug,needs testing')
    expect(toggleLabel('bug,needs testing', 'BUG')).toBe('needs testing')
    expect(toggleLabel('bug', 'bug')).toBeUndefined()
    expect(splitLabels(' a , ,b ')).toEqual(['a', 'b'])
  })
  it('detects filters', () => {
    expect(hasFilters({ state: 'closed', sort: 'created' })).toBe(false)
    expect(hasFilters({ assignee: 'none' })).toBe(true)
  })
})

const ev = (type: string, data: Record<string, unknown>) => ({ id: 1, type, data, created_at: '' }) as unknown as PullEvent

describe('issueEventText', () => {
  it('describes issue events', () => {
    expect(issueEventText(ev('closed', { reason: 'not_planned' }))).toBe('closed this as not planned')
    expect(issueEventText(ev('closed', { reason: 'completed', pull: 3, commit: 'abc' }))).toBe('closed this in')
    expect(issueEventText(ev('closed', { reason: 'completed', commit: 'abc' }))).toBe('closed this in commit')
    expect(issueEventText(ev('milestoned', { milestone: 'v1' }))).toBe('added this to the v1 milestone')
    expect(issueEventText(ev('assigned', { assignee: 'mia' }))).toBe('assigned mia')
    expect(issueEventText(ev('pushed', {}))).toBeNull()
  })
})

describe('milestones', () => {
  it('computes progress and overdue', () => {
    expect(milestoneProgress({ open_issues: 0, closed_issues: 0 })).toBe(0)
    expect(milestoneProgress({ open_issues: 1, closed_issues: 3 })).toBe(75)
    const now = new Date('2026-11-13T12:00:00Z')
    expect(isOverdue('2026-11-12', 'open', now)).toBe(true)
    expect(isOverdue('2026-11-13', 'open', now)).toBe(false)
    expect(isOverdue('2026-11-12', 'closed', now)).toBe(false)
    expect(isOverdue(null, 'open', now)).toBe(false)
  })
})
