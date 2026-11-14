import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useRouterState } from '@tanstack/react-router'
import { useMemo } from 'react'
import { api } from '../api/client'
import type { Session } from '../api/client'
import { reposQuery } from '../api/queries'
import { canTriage, canWrite } from './permissions'
import { loadVisits, pickRecent } from './recent'
import { setThemePref } from './theme'

export type CommandGroup = 'Navigate' | 'Repository' | 'Repositories' | 'Account' | 'Appearance' | 'Admin'

export type CommandIcon =
  | 'home'
  | 'repo'
  | 'pull'
  | 'issue'
  | 'ci'
  | 'settings'
  | 'logout'
  | 'theme'
  | 'admin'
  | 'release'
  | 'label'
  | 'milestone'
  | 'webhook'
  | 'cache'
  | 'plus'
  | 'keyboard'

export type Command = {
  id: string
  label: string
  group: CommandGroup
  /** Two-key "g x" style shortcut shown in the palette. */
  shortcut?: string
  icon?: CommandIcon
  run: () => void
}

/** Display order of groups when the palette has no query. */
export const groupOrder: CommandGroup[] = ['Repository', 'Navigate', 'Repositories', 'Account', 'Appearance', 'Admin']

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
export function useCommands(session: Session | null | undefined, extra: { openShortcuts: () => void }): Command[] {
  const navigate = useNavigate()
  const logout = useLogout()
  const isAdmin = session?.user.role === 'admin'
  const repos = useQuery({ ...reposQuery, enabled: !!session })
  // The repository currently shown, for repository-scoped commands.
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const current = (repos.data ?? []).find((r) => pathname.split('/')[1]?.toLowerCase() === r.name.toLowerCase())
  const { openShortcuts } = extra
  return useMemo(() => {
    const go = (to: string) => () => void navigate({ to })
    const cmds: Command[] = [
      { id: 'home', label: 'Home', group: 'Navigate', shortcut: 'g h', icon: 'home', run: go('/') },
      { id: 'repos', label: 'Repositories', group: 'Navigate', shortcut: 'g r', icon: 'repo', run: go('/repos') },
      { id: 'pulls', label: 'Pull requests', group: 'Navigate', shortcut: 'g p', icon: 'pull', run: go('/pulls') },
      { id: 'issues', label: 'Issues', group: 'Navigate', shortcut: 'g i', icon: 'issue', run: go('/issues') },
      { id: 'ci', label: 'CI runs', group: 'Navigate', shortcut: 'g c', icon: 'ci', run: go('/ci') },
      { id: 'settings', label: 'Settings', group: 'Account', shortcut: 'g s', icon: 'settings', run: go('/settings') },
      { id: 'shortcuts', label: 'Keyboard shortcuts', group: 'Account', icon: 'keyboard', run: openShortcuts },
      { id: 'logout', label: 'Sign out', group: 'Account', icon: 'logout', run: () => void logout() },
      { id: 'theme-dark', label: 'Theme: Dark', group: 'Appearance', icon: 'theme', run: () => setThemePref('dark') },
      { id: 'theme-light', label: 'Theme: Light', group: 'Appearance', icon: 'theme', run: () => setThemePref('light') },
      { id: 'theme-system', label: 'Theme: System', group: 'Appearance', icon: 'theme', run: () => setThemePref('system') },
    ]
    if (isAdmin) {
      cmds.push(
        { id: 'admin-users', label: 'Admin: Users', group: 'Admin', shortcut: 'g a', icon: 'admin', run: go('/admin/users') },
        { id: 'admin-health', label: 'Admin: Health', group: 'Admin', icon: 'admin', run: go('/admin/health') },
        { id: 'admin-org', label: 'Admin: Organization', group: 'Admin', icon: 'admin', run: go('/admin/org') },
        { id: 'admin-runners', label: 'Admin: Runners', group: 'Admin', icon: 'admin', run: go('/admin/runners') },
        { id: 'admin-ci', label: 'Admin: CI settings', group: 'Admin', icon: 'admin', run: go('/admin/ci') },
        { id: 'admin-webhooks', label: 'Admin: Webhooks', group: 'Admin', icon: 'admin', run: go('/admin/webhooks') },
        { id: 'admin-audit', label: 'Admin: Audit log', group: 'Admin', icon: 'admin', run: go('/admin/audit') },
      )
    }
    if (current) {
      const repo = current.name
      const to = (path: string, icon: CommandIcon, label: string, id: string): Command => ({
        id,
        label: `${repo}: ${label}`,
        group: 'Repository',
        icon,
        run: () => void navigate({ to: path, params: { repo, _splat: '' } }),
      })
      const scoped: Command[] = []
      if (canWrite(current.permission) && !current.archived) scoped.push(to('/$repo/compare/$', 'plus', 'New pull request', 'repo-new-pull'))
      if (canTriage(current.permission) && !current.archived) scoped.push(to('/$repo/issues/new', 'plus', 'New issue', 'repo-new-issue'))
      if (canWrite(current.permission) && !current.archived) scoped.push(to('/$repo/releases/new', 'plus', 'New release', 'repo-new-release'))
      scoped.push(
        to('/$repo', 'repo', 'Code', 'repo-code'),
        to('/$repo/issues', 'issue', 'Issues', 'repo-issues'),
        to('/$repo/pulls', 'pull', 'Pull requests', 'repo-pulls'),
        to('/$repo/ci', 'ci', 'CI runs', 'repo-ci'),
        to('/$repo/releases', 'release', 'Releases', 'repo-releases'),
        to('/$repo/labels', 'label', 'Labels', 'repo-labels'),
        to('/$repo/milestones', 'milestone', 'Milestones', 'repo-milestones'),
        to('/$repo/ci/caches', 'cache', 'CI caches', 'repo-caches'),
      )
      if (canWrite(current.permission)) {
        scoped.push(to('/$repo/settings', 'settings', 'Settings', 'repo-settings'), to('/$repo/settings/webhooks', 'webhook', 'Webhooks', 'repo-webhooks'))
      }
      cmds.unshift(...scoped)
    }
    // Recently visited first, then the rest.
    const all = repos.data ?? []
    const recent = pickRecent(loadVisits(), all, all.length)
    for (const r of recent) {
      cmds.push({ id: `repo:${r.name}`, label: r.name, group: 'Repositories', icon: 'repo', run: () => void navigate({ to: '/$repo', params: { repo: r.name } }) })
    }
    for (const r of all) {
      if (!recent.includes(r)) cmds.push({ id: `repo:${r.name}`, label: r.name, group: 'Repositories', icon: 'repo', run: () => void navigate({ to: '/$repo', params: { repo: r.name } }) })
    }
    return cmds
  }, [navigate, logout, isAdmin, repos.data, current, openShortcuts])
}
