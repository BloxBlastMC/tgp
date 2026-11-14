import { describe, expect, it } from 'vitest'
import {
  appendLines,
  checksHeadline,
  buildLogItems,
  defaultJob,
  durationText,
  formatBytes,
  formatDuration,
  groupJobs,
  groupKind,
  linesByStep,
  parseAnsi,
  refLabel,
  statusKind,
  stripAnsi,
  validateRunList,
  validateRunPage,
} from './ci'

const line = (n: number, text: string, step = 1) => ({ n, step, t: '2026-11-13T00:00:00Z', text })

describe('statusKind', () => {
  it('maps status and conclusion', () => {
    expect(statusKind('queued')).toBe('queued')
    expect(statusKind('in_progress')).toBe('running')
    expect(statusKind('waiting')).toBe('waiting')
    expect(statusKind('pending')).toBe('pending')
    expect(statusKind('completed', 'success')).toBe('success')
    expect(statusKind('completed', 'timed_out')).toBe('timed_out')
    expect(statusKind('completed', null)).toBe('failure')
  })
})

describe('durations', () => {
  it('formats', () => {
    expect(formatDuration(0)).toBe('0s')
    expect(formatDuration(45_900)).toBe('45s')
    expect(formatDuration(192_000)).toBe('3m 12s')
    expect(formatDuration(3_840_000)).toBe('1h 4m')
    expect(formatDuration(-5)).toBe('0s')
  })
  it('measures between timestamps or until now', () => {
    expect(durationText('2026-01-01T00:00:00Z', '2026-01-01T00:01:05Z')).toBe('1m 5s')
    expect(durationText('2026-01-01T00:00:00Z', null, Date.parse('2026-01-01T00:00:10Z'))).toBe('10s')
    expect(durationText(null, null)).toBe('')
  })
  it('formats bytes', () => {
    expect(formatBytes(4 * 2 ** 30)).toBe('4 GiB')
    expect(formatBytes(512 * 2 ** 20)).toBe('512 MiB')
  })
})

describe('refLabel', () => {
  it('shortens refs', () => {
    expect(refLabel('refs/heads/main')).toBe('main')
    expect(refLabel('refs/tags/v1')).toBe('v1')
    expect(refLabel('refs/pull/3/head')).toBe('#3')
    expect(refLabel('refs/heads/x', 7)).toBe('#7')
  })
})

describe('search validation', () => {
  it('keeps known values only', () => {
    expect(validateRunList({ workflow: 'ci.yml', status: 'failure', event: 'push', page: '2', branch: '' })).toEqual({ workflow: 'ci.yml', status: 'failure', event: 'push', page: 2 })
    expect(validateRunList({ status: 'bogus', event: 'x', page: '1' })).toEqual({})
    expect(validateRunPage({ job: '12' })).toEqual({ job: 12 })
    expect(validateRunPage({ job: 'x' })).toEqual({})
  })
})

describe('parseAnsi', () => {
  it('splits colours and bold', () => {
    expect(parseAnsi('plain')).toEqual([{ text: 'plain' }])
    expect(parseAnsi('\x1b[31mred\x1b[0m ok')).toEqual([{ text: 'red', fg: 1 }, { text: ' ok' }])
    expect(parseAnsi('\x1b[1;92mB\x1b[22mn\x1b[39m')).toEqual([
      { text: 'B', fg: 10, bold: true },
      { text: 'n', fg: 10 },
    ])
    expect(parseAnsi('\x1b[44mbg\x1b[49m')).toEqual([{ text: 'bg', bg: 4 }])
  })
  it('drops other escapes and handles 256-colour codes', () => {
    expect(parseAnsi('a\x1b[2Kb')).toEqual([{ text: 'ab' }])
    expect(parseAnsi('\x1b[38;5;3mx\x1b[38;5;200my')).toEqual([{ text: 'xy', fg: 3 }])
    expect(stripAnsi('\x1b[31mred\x1b[0m')).toBe('red')
  })
})

describe('buildLogItems', () => {
  it('folds groups and tags commands', () => {
    const items = buildLogItems([
      line(1, 'start'),
      line(2, '::group::Install'),
      line(3, 'a'),
      line(4, 'b'),
      line(5, '::endgroup::'),
      line(6, '::error file=x.go::boom'),
      line(7, '::group::Unclosed'),
      line(8, 'c'),
      line(9, '::group::Next'),
      line(10, 'd'),
    ])
    expect(items.map((i) => i.kind)).toEqual(['line', 'group', 'line', 'group', 'group'])
    const g = items[1]
    expect(g?.kind === 'group' && g.title).toBe('Install')
    expect(g?.kind === 'group' && g.children.map((c) => c.text)).toEqual(['a', 'b'])
    const e = items[2]
    expect(e?.kind === 'line' && e.level).toBe('error')
    expect(e?.kind === 'line' && e.line.text).toBe('boom')
    expect(items[4]?.kind === 'group' && items[4].children.length).toBe(1)
  })
})

describe('lines', () => {
  it('groups by step and appends without duplicates', () => {
    const a = [line(1, 'x', 0), line(2, 'y', 1)]
    expect([...linesByStep(a).keys()]).toEqual([0, 1])
    expect(appendLines(a, [line(2, 'y'), line(3, 'z')]).map((l) => l.n)).toEqual([1, 2, 3])
    expect(appendLines(a, [line(1, 'x')])).toBe(a)
  })
})

describe('jobs', () => {
  const job = (id: number, key: string, status: string, conclusion: string | null = null) =>
    ({ id, key, status, conclusion }) as unknown as Parameters<typeof groupJobs>[0][number]
  it('groups by key and picks status and default job', () => {
    const jobs = [job(1, 'test', 'completed', 'success'), job(2, 'lint', 'in_progress'), job(3, 'test', 'completed', 'failure')]
    const g = groupJobs(jobs)
    expect(g.map((x) => [x.key, x.jobs.length])).toEqual([
      ['test', 2],
      ['lint', 1],
    ])
    expect(groupKind(g[0]!.jobs)).toBe('failure')
    expect(groupKind(g[1]!.jobs)).toBe('running')
    expect(defaultJob(jobs)?.id).toBe(3)
    expect(defaultJob([job(1, 'a', 'queued'), job(2, 'b', 'in_progress')])?.id).toBe(2)
  })
})

describe('checksHeadline', () => {
  const base = { total: 0, succeeded: 0, failed: 0, pending: 0, skipped: 0, checks: [], missing_required: [] as string[] }
  it('summarises', () => {
    expect(checksHeadline({ ...base, state: 'none' })).toBe('No checks')
    expect(checksHeadline({ ...base, state: 'failure', total: 3, failed: 1, succeeded: 2 })).toBe('Some checks were not successful · 1 failing, 2 successful')
    expect(checksHeadline({ ...base, state: 'pending', missing_required: ['CI / test'] })).toBe('Some checks haven’t completed yet · 1 expected')
    expect(checksHeadline({ ...base, state: 'success', total: 1, succeeded: 1 })).toBe('All checks have passed · 1 successful')
  })
})
