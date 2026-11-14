import { describe, expect, it } from 'vitest'
import { issueState, pullState } from './states'

describe('pullState', () => {
  it('distinguishes draft, open, merged and closed', () => {
    expect(pullState({ state: 'open' })).toMatchObject({ key: 'open', tone: 'success' })
    expect(pullState({ state: 'open', draft: true })).toMatchObject({ key: 'draft', label: 'Draft' })
    expect(pullState({ state: 'merged', draft: true })).toMatchObject({ key: 'merged', tone: 'done' })
    expect(pullState({ state: 'closed' })).toMatchObject({ key: 'closed', tone: 'danger' })
  })
})

describe('issueState', () => {
  it('uses the close reason', () => {
    expect(issueState({ state: 'open' }).label).toBe('Open')
    expect(issueState({ state: 'closed', state_reason: 'completed' })).toMatchObject({ key: 'completed', tone: 'done' })
    expect(issueState({ state: 'closed', state_reason: 'not_planned' }).label).toBe('Closed as not planned')
  })
})
