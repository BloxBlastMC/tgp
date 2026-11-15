import { describe, expect, it } from 'vitest'
import { auditActionLabel, auditActionTone, healthCheckLabel, humanize, versionLabel } from './labels'

describe('labels', () => {
  it('humanizes unknown keys', () => {
    expect(humanize('foo_bar.baz')).toBe('Foo bar baz')
    expect(healthCheckLabel('ci_queue')).toBe('CI queue')
    expect(healthCheckLabel('new_thing')).toBe('New thing')
    expect(auditActionLabel('account.password_change')).toBe('Password changed')
  })
  it('tones audit actions', () => {
    expect(auditActionTone('repo.delete')).toBe('danger')
    expect(auditActionTone('user.create')).toBe('warning')
    expect(auditActionTone('auth.login')).toBe('neutral')
  })
  it('formats versions', () => {
    expect(versionLabel('dev', 'unknown')).toBe('development build')
    expect(versionLabel('', '')).toBe('development build')
    expect(versionLabel('1.2.0', 'abcdef123')).toBe('v1.2.0 (abcdef1)')
    expect(versionLabel('v1.2.0')).toBe('v1.2.0')
  })
})
