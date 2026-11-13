import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useMemo } from 'react'
import { api } from '../api/client'
import type { Session } from '../api/client'
import { reposQuery } from '../api/queries'

export type Command = {
  id: string
  label: string
  group: 'Navigate' | 'Repository' | 'Account' | 'Appearance'
  /** Two-key "g x" style shortcut shown in the palette. */
  shortcut?: string
  run: () => void
}

export function useLogout() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  return async () => {
    await api.POST('/auth/logout')
    qc.setQueryData(['session'], null)
    qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'meta' && q.queryKey[0] !== 'session' })
    void navigate({ to: '/login' })
  }
}

/** All palette commands available to the current user. */
export function useCommands(session: Session | null | undefined, toggleTheme: () => void): Command[] {
  const navigate = useNavigate()
  const logout = useLogout()
  const isAdmin = session?.user.role === 'admin'
  const repos = useQuery({ ...reposQuery, enabled: !!session })
  return useMemo(() => {
    const go = (to: string) => () => void navigate({ to })
    const cmds: Command[] = [
      { id: 'repos', label: 'Repositories', group: 'Navigate', shortcut: 'g r', run: go('/') },
      { id: 'pulls', label: 'Pull requests', group: 'Navigate', shortcut: 'g p', run: go('/pulls') },
      { id: 'issues', label: 'Issues', group: 'Navigate', shortcut: 'g i', run: go('/issues') },
      { id: 'ci', label: 'CI runs', group: 'Navigate', shortcut: 'g c', run: go('/ci') },
      { id: 'settings', label: 'Settings', group: 'Account', shortcut: 'g s', run: go('/settings') },
      { id: 'theme', label: 'Toggle light/dark theme', group: 'Appearance', run: toggleTheme },
      { id: 'logout', label: 'Sign out', group: 'Account', run: () => void logout() },
    ]
    if (isAdmin) {
      cmds.splice(4, 0,
        { id: 'admin-users', label: 'Admin: Users', group: 'Navigate', shortcut: 'g a', run: go('/admin/users') },
        { id: 'admin-org', label: 'Admin: Organization', group: 'Navigate', run: go('/admin/org') },
        { id: 'admin-audit', label: 'Admin: Audit log', group: 'Navigate', run: go('/admin/audit') },
      )
    }
    for (const r of repos.data ?? []) {
      cmds.push({ id: `repo:${r.name}`, label: r.name, group: 'Repository', run: () => void navigate({ to: '/$repo', params: { repo: r.name } }) })
    }
    return cmds
  }, [navigate, logout, isAdmin, toggleTheme, repos.data])
}
