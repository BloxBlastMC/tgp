import { describe, expect, it } from 'vitest'
import { compactNumber, dayKey, dueLabel, formatBytes, greeting, plural, relativeTime, shortSha } from './format'

describe('formatBytes', () => {
  it('uses binary units', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KiB')
    expect(formatBytes(200 * 1024)).toBe('200 KiB')
    expect(formatBytes(12.5 * 2 ** 20)).toBe('12.5 MiB')
    expect(formatBytes(4 * 2 ** 30)).toBe('4 GiB')
  })
})

describe('relativeTime', () => {
  const now = Date.parse('2026-11-14T12:00:00Z')
  it('describes past and future times', () => {
    expect(relativeTime(null, now)).toBe('never')
    expect(relativeTime('2026-11-14T11:59:50Z', now)).toBe('just now')
    expect(relativeTime('2026-11-14T11:00:00Z', now)).toMatch(/hour/)
    expect(relativeTime('2026-11-16T12:00:00Z', now)).toMatch(/2 days|in 2/)
  })
})

describe('dueLabel', () => {
  const now = new Date('2026-11-14T12:00:00')
  it('says overdue by days, not by a date', () => {
    expect(dueLabel('2026-11-11', 'open', now)).toEqual({ text: 'Overdue by 3 days', overdue: true })
    expect(dueLabel('2026-11-13', 'open', now)).toEqual({ text: 'Overdue by 1 day', overdue: true })
  })
  it('counts down near dates', () => {
    expect(dueLabel(dayKey(now.toISOString()), 'open', now)?.text).toBe('Due today')
    expect(dueLabel('2026-11-15', 'open', now)?.text).toBe('Due tomorrow')
    expect(dueLabel('2026-11-19', 'open', now)?.text).toBe('Due in 5 days')
  })
  it('shows the date when far away or closed', () => {
    expect(dueLabel('2027-01-31', 'open', now)?.text).toMatch(/^Due .*2027/)
    expect(dueLabel('2026-11-01', 'closed', now)).toMatchObject({ overdue: false })
    expect(dueLabel(null, 'open', now)).toBeNull()
  })
})

describe('small helpers', () => {
  it('greets by time of day', () => {
    expect(greeting(9)).toBe('Good morning')
    expect(greeting(14)).toBe('Good afternoon')
    expect(greeting(20)).toBe('Good evening')
  })
  it('pluralizes and compacts', () => {
    expect(plural(1, 'file')).toBe('1 file')
    expect(plural(3, 'file')).toBe('3 files')
    expect(compactNumber(999)).toBe('999')
    expect(compactNumber(1234)).toBe('1.2k')
    expect(compactNumber(45678)).toBe('46k')
    expect(shortSha('0123456789abcdef')).toBe('0123456')
  })
})
