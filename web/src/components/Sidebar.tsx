import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  ChevronsUpDown,
  CircleDot,
  FolderGit2,
  GitPullRequest,
  Home,
  Keyboard,
  LogOut,
  Monitor,
  Moon,
  Play,
  Plus,
  Search,
  Settings,
  Shield,
  Sun,
} from 'lucide-react'
import { useEffect, useState, type ComponentType } from 'react'
import { metaQuery, myIssuesQuery, myPullsQuery, reposQuery, sessionQuery } from '../api/queries'
import { useLogout } from '../lib/commands'
import { loadVisits, pickRecent } from '../lib/recent'
import { useTheme } from '../lib/theme'
import { CreateRepoDialog } from './repos/CreateRepoDialog'
import { RepoChooser, type RepoIntent } from './repos/RepoChooser'
import { Avatar, Counter, IconButton, Kbd, Menu, MenuItem, MenuLabel, MenuRadioItem, MenuSeparator } from './ui'

type NavItem = {
  to: string
  label: string
  icon: ComponentType<{ size?: number; className?: string }>
  exact?: boolean
  badge?: number
  badgeLabel?: string
}

const navLinkClass =
  'group flex h-7 items-center gap-2.5 rounded-md px-2 text-base text-fg-muted transition-colors hover:bg-bg-hover hover:text-fg data-[status=active]:bg-bg-active data-[status=active]:font-medium data-[status=active]:text-fg'

function NavLink({ to, label, icon: Icon, exact, badge, badgeLabel }: NavItem) {
  return (
    <Link to={to} activeOptions={{ exact }} className={navLinkClass}>
      <Icon size={15} className="shrink-0 text-fg-faint group-hover:text-fg-muted group-data-[status=active]:text-fg" />
      <span className="flex-1 truncate">{label}</span>
      {!!badge && (
        <Counter tone="accent" title={badgeLabel} aria-label={badgeLabel}>
          {badge}
        </Counter>
      )}
    </Link>
  )
}

function useRecentRepos(enabled: boolean) {
  const repos = useQuery({ ...reposQuery, enabled })
  const [visits, setVisits] = useState(loadVisits)
  useEffect(() => {
    const on = () => setVisits(loadVisits())
    window.addEventListener('tgp-recent', on)
    window.addEventListener('storage', on)
    return () => {
      window.removeEventListener('tgp-recent', on)
      window.removeEventListener('storage', on)
    }
  }, [])
  return pickRecent(visits, repos.data ?? [], 5)
}

export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

export function Sidebar({ openPalette, openShortcuts }: { openPalette: () => void; openShortcuts: () => void }) {
  const meta = useQuery(metaQuery)
  const session = useQuery(sessionQuery)
  const logout = useLogout()
  const navigate = useNavigate()
  const { pref, setPref } = useTheme()
  const user = session.data?.user
  const isAdmin = user?.role === 'admin'
  const reviews = useQuery({ ...myPullsQuery('review_requested'), enabled: !!user, refetchInterval: 60_000 })
  const assigned = useQuery({ ...myIssuesQuery('assigned'), enabled: !!user, refetchInterval: 120_000 })
  const recent = useRecentRepos(!!user)
  const [creating, setCreating] = useState(false)
  const [choose, setChoose] = useState<RepoIntent | null>(null)
  const orgName = meta.data?.org_name || 'tgp'

  const review = reviews.data?.total ?? 0
  const assignedCount = assigned.data?.total ?? 0
  const primary: NavItem[] = [
    { to: '/', label: 'Home', icon: Home, exact: true },
    { to: '/repos', label: 'Repositories', icon: FolderGit2 },
    { to: '/pulls', label: 'Pull requests', icon: GitPullRequest, badge: review, badgeLabel: `${review} waiting for your review` },
    { to: '/issues', label: 'Issues', icon: CircleDot, badge: assignedCount, badgeLabel: `${assignedCount} assigned to you` },
    { to: '/ci', label: 'CI', icon: Play },
  ]

  return (
    <nav aria-label="Main" className="flex w-60 shrink-0 flex-col border-r border-border bg-bg-sidebar">
      <div className="flex h-12 shrink-0 items-center gap-2 px-3">
        <Link to="/" className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 py-1 hover:bg-bg-hover">
          <img src={meta.data?.has_logo ? '/api/v1/org/logo' : '/favicon.svg'} alt="" className="size-6 shrink-0 rounded-md object-cover" />
          <span className="truncate font-semibold tracking-tight">{orgName}</span>
        </Link>
        <Menu
          placement="bottom-end"
          label="Create"
          trigger={(t) => (
            <IconButton {...t} label="Create new" size="sm" variant="secondary">
              <Plus size={14} />
            </IconButton>
          )}
        >
          {isAdmin && (
            <MenuItem icon={<FolderGit2 size={14} />} onSelect={() => setCreating(true)}>
              New repository
            </MenuItem>
          )}
          <MenuItem icon={<CircleDot size={14} />} onSelect={() => setChoose('issue')}>
            New issue…
          </MenuItem>
          <MenuItem icon={<GitPullRequest size={14} />} onSelect={() => setChoose('pull')}>
            New pull request…
          </MenuItem>
        </Menu>
      </div>
      <div className="shrink-0 px-3 pb-2">
        <button
          type="button"
          onClick={openPalette}
          className="flex h-8 w-full items-center gap-2 rounded-md border border-border bg-bg px-2 text-fg-faint shadow-card transition-colors hover:border-border-strong hover:text-fg-muted"
        >
          <Search size={13} />
          <span className="flex-1 text-left">Search…</span>
          <span className="flex gap-0.5">
            <Kbd>{isMac ? '⌘' : 'Ctrl'}</Kbd>
            <Kbd>K</Kbd>
          </span>
        </button>
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-3 py-2">
        <div className="flex flex-col gap-0.5">
          {primary.map((n) => (
            <NavLink key={n.to} {...n} />
          ))}
        </div>

        {recent.length > 0 && (
          <div className="flex flex-col gap-0.5">
            <div className="mb-1 px-2 text-xs font-semibold tracking-wide text-fg-faint uppercase">Recent</div>
            {recent.map((r) => (
              <Link key={r.id} to="/$repo" params={{ repo: r.name }} activeOptions={{ exact: false }} className={navLinkClass}>
                <span
                  className="flex size-4 shrink-0 items-center justify-center rounded text-2xs font-semibold text-white uppercase"
                  style={{ backgroundColor: `var(--avatar-${(r.id % 8) + 1})` }}
                  aria-hidden
                >
                  {r.name.slice(0, 1)}
                </span>
                <span className="truncate">{r.name}</span>
              </Link>
            ))}
          </div>
        )}

        {isAdmin && (
          <div className="mt-auto flex flex-col gap-0.5">
            <NavLink to="/admin" label="Admin" icon={Shield} />
          </div>
        )}
      </div>

      <div className="shrink-0 border-t border-border p-2">
        {user && (
          <Menu
            placement="top-start"
            label="Account"
            className="w-56"
            trigger={(t) => (
              <button
                type="button"
                {...t}
                aria-label="Account menu"
                className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-bg-hover aria-expanded:bg-bg-hover"
              >
                <Avatar user={user} size={26} title={false} />
                <span className="min-w-0 flex-1 leading-tight">
                  <span className="block truncate font-medium">{user.display_name}</span>
                  <span className="block truncate text-xs text-fg-faint capitalize">{user.role}</span>
                </span>
                <ChevronsUpDown size={13} className="shrink-0 text-fg-faint" />
              </button>
            )}
          >
            <div className="px-2 py-1.5">
              <div className="truncate font-medium">{user.display_name}</div>
              <div className="truncate text-sm text-fg-faint">{user.email || user.username}</div>
            </div>
            <MenuSeparator />
            <MenuItem icon={<Settings size={14} />} onSelect={() => void navigate({ to: '/settings' })} shortcut="G S">
              Settings
            </MenuItem>
            <MenuItem icon={<Keyboard size={14} />} onSelect={openShortcuts} shortcut="?">
              Keyboard shortcuts
            </MenuItem>
            <MenuSeparator />
            <MenuLabel>Theme</MenuLabel>
            <MenuRadioItem checked={pref === 'system'} onSelect={() => setPref('system')} icon={<Monitor size={14} />}>
              System
            </MenuRadioItem>
            <MenuRadioItem checked={pref === 'dark'} onSelect={() => setPref('dark')} icon={<Moon size={14} />}>
              Dark
            </MenuRadioItem>
            <MenuRadioItem checked={pref === 'light'} onSelect={() => setPref('light')} icon={<Sun size={14} />}>
              Light
            </MenuRadioItem>
            <MenuSeparator />
            <MenuItem icon={<LogOut size={14} />} onSelect={() => void logout()}>
              Sign out
            </MenuItem>
            <div className="px-2 pt-1 pb-0.5 text-2xs text-fg-faint">tgp {meta.data?.version ?? ''}</div>
          </Menu>
        )}
      </div>
      {isAdmin && <CreateRepoDialog open={creating} onClose={() => setCreating(false)} />}
      <RepoChooser intent={choose} onClose={() => setChoose(null)} />
    </nav>
  )
}
