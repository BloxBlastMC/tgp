import { describe, expect, it } from 'vitest'
import { atLeast, canTriage, canWrite, isRepoAdmin } from './permissions'

describe('permissions', () => {
  it('orders read < triage < write < admin', () => {
    expect(canTriage('read')).toBe(false)
    expect(canTriage('triage')).toBe(true)
    expect(canWrite('triage')).toBe(false)
    expect(canWrite('write')).toBe(true)
    expect(canWrite('admin')).toBe(true)
    expect(isRepoAdmin('write')).toBe(false)
    expect(isRepoAdmin('admin')).toBe(true)
  })
  it('treats unknown and missing as no access', () => {
    expect(atLeast(undefined, 'read')).toBe(false)
    expect(atLeast('owner', 'read')).toBe(false)
    expect(atLeast('read', 'read')).toBe(true)
  })
})
