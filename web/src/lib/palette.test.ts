import { describe, expect, it } from 'vitest'
import { filterCommands } from './palette'

const cmds = [
  { label: 'Admin: CI settings', group: 'Admin' },
  { label: 'Admin: Runners', group: 'Admin' },
  { label: 'Settings', group: 'Account' },
  { label: 'game: CI runs', group: 'Repositories' },
]

describe('filterCommands', () => {
  it('puts exact and prefix matches first', () => {
    expect(filterCommands(cmds, 'settings').map((c) => c.label)).toEqual(['Settings', 'Admin: CI settings'])
    expect(filterCommands(cmds, 'runners')[0]?.label).toBe('Admin: Runners')
    expect(filterCommands(cmds, 'ci').map((c) => c.label)).toEqual(['Admin: CI settings', 'game: CI runs'])
  })
  it('matches groups last and keeps order without a query', () => {
    expect(filterCommands(cmds, 'account').map((c) => c.label)).toEqual(['Settings'])
    expect(filterCommands(cmds, '  ')).toEqual(cmds)
    expect(filterCommands(cmds, 'zzz')).toEqual([])
  })
})
