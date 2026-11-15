import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useRouterState } from '@tanstack/react-router'
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
  X,
} from 'lucide-react'
import { useEffect, useState, type ComponentType } from 'react'
import { metaQuery, myIssuesQuery, myPullsQuery, reposQuery, sessionQuery } from '../api/queries'
import { isMac, useLogout } from '../lib/commands'
import { versionLabel } from '../lib/labels'
import { useMediaQuery } from '../lib/useMediaQuery'
import { loadVisits, pickRecent } from '../lib/recent'
import { useTheme } from '../lib/theme'
import { CreateRepoDialog } from './repos/CreateRepoDialog'
import { RepoChooser, type RepoIntent } from './repos/RepoChooser'
import { Avatar, Counter, cx, IconButton, Kbd, Menu, MenuItem, MenuLabel, MenuRadioItem, MenuSeparator, Tooltip } from './ui'

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

function NavLink({ to, label, icon: Icon, exact, badge, badgeLabel, rail }: NavItem & { rail?: boolean }) {
  const link = (
    <Link to={to} activeOptions={{ exact }} aria-label={rail ? (badge ? `${label} (${badgeLabel})` : label) : undefined} className={cx(navLinkClass, rail && 'relative justify-center px-0')}>
      <Icon size={15} className="shrink-0 text-fg-faint group-hover:text-fg-muted group-data-[status=active]:text-fg" />
      {!rail && <span className="flex-1 truncate">{label}</span>}
      {!!badge &&
        (rail ? (
          <span className="absolute top-0.5 right-1 size-2 rounded-full bg-accent-solid" aria-hidden />
        ) : (
          <Tooltip content={badgeLabel} describe={false}>
            <Counter tone="accent" aria-label={badgeLabel}>
              {badge}
            </Counter>
          </Tooltip>
        ))}
    </Link>
  )
  if (!rail) return link
  return (
    <Tooltip content={badge ? `${label} · ${badgeLabel}` : label} side="right" describe={false}>
      {link}
    </Tooltip>
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

export { isMac }

/**
 * The main navigation. Full width from 1024px, an icon rail between 768
 * and 1024px, and an off-canvas drawer (opened from the mobile top bar)
 * below 768px.
 */
export function Sidebar({
  openPalette,
  openShortcuts,
  mobileOpen = false,
  onCloseMobile,
}: {
  openPalette: () => void
  openShortcuts: () => void
  mobileOpen?: boolean
  onCloseMobile?: () => void
}) {
  const railQuery = useMediaQuery('(min-width: 768px) and (max-width: 1023.98px)')
  const rail = railQuery && !mobileOpen
  const pathname = useRouterState({ select: (st) => st.location.pathname })
  useEffect(() => {
    onCloseMobile?.()
    // Close the drawer after navigating.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname])
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
    { to: '/pulls', label: 'Pull requests', icon: GitPullRequest, badge: review, badgeLabel: `${review} review${review === 1 ? '' : 's'} requested` },
    { to: '/issues', label: 'Issues', icon: CircleDot, badge: assignedCount, badgeLabel: `${assignedCount} assigned to you` },
    { to: '/ci', label: 'Runs', icon: Play },
  ]

  return (
    <>
    {mobileOpen && <div className="fixed inset-0 z-30 bg-black/40 md:hidden" onClick={onCloseMobile} aria-hidden />}
    <nav
      aria-label="Main"
      className={cx(
        'shrink-0 flex-col border-r border-border bg-bg-sidebar',
        mobileOpen ? 'fixed inset-y-0 left-0 z-40 flex w-72 shadow-popover' : 'hidden',
        'md:static md:z-auto md:flex md:w-14 md:shadow-none lg:w-60',
      )}
    >
      <div className={cx('flex h-12 shrink-0 items-center gap-2', rail ? 'flex-col justify-center px-2' : 'px-3')}>
        {!rail && (
          <Link to="/" className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 py-1 hover:bg-bg-hover">
            <img src={meta.data?.has_logo ? '/api/v1/org/logo' : '/favicon.svg'} alt="" className="size-6 shrink-0 rounded-md object-cover" />
            <span className="truncate font-semibold tracking-tight">{orgName}</span>
          </Link>
        )}
        {rail && (
          <Tooltip content={orgName} side="right" describe={false}>
            <Link to="/" aria-label={`${orgName} home`} className="rounded-md p-1 hover:bg-bg-hover">
              <img src={meta.data?.has_logo ? '/api/v1/org/logo' : '/favicon.svg'} alt="" className="size-6 rounded-md object-cover" />
            </Link>
          </Tooltip>
        )}
        {mobileOpen && (
          <IconButton label="Close navigation" size="sm" onClick={onCloseMobile} className="md:hidden">
            <X size={14} />
          </IconButton>
        )}
        {!rail && (
        <Menu
          placement="bottom-end"
          label="Create"
          trigger={(t) => (
            <IconButton {...t} label="Create…" size="sm" variant="secondary">
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
        )}
      </div>
      <div className={cx('shrink-0 pb-2', rail ? 'flex justify-center px-2' : 'px-3')}>
        {rail ? (
          <IconButton label={`Search (${isMac ? '⌘' : 'Ctrl'}+K)`} onClick={openPalette} tooltip={`Search · ${isMac ? '⌘' : 'Ctrl'}+K`}>
            <Search size={15} />
          </IconButton>
        ) : (
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
        )}
      </div>

      <div className={cx('flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto py-2', rail ? 'px-2' : 'px-3')}>
        <div className="flex flex-col gap-0.5">
          {primary.map((n) => (
            <NavLink key={n.to} {...n} rail={rail} />
          ))}
        </div>

        {recent.length > 0 && !rail && (
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
            <NavLink to="/admin" label="Admin" icon={Shield} rail={rail} />
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
                className={cx('flex w-full items-center gap-2.5 rounded-md py-1.5 text-left transition-colors hover:bg-bg-hover aria-expanded:bg-bg-hover', rail ? 'justify-center px-0' : 'px-2')}
              >
                <Avatar user={user} size={26} title={false} />
                {!rail && (
                  <>
                    <span className="min-w-0 flex-1 leading-tight">
                      <span className="block truncate font-medium">{user.display_name}</span>
                      <span className="block truncate text-xs text-fg-faint capitalize">{user.role}</span>
                    </span>
                    <ChevronsUpDown size={13} className="shrink-0 text-fg-faint" />
                  </>
                )}
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
            <MenuRadioItem checked={pref === 'light'} onSelect={() => setPref('light')} icon={<Sun size={14} />}>
              Light
            </MenuRadioItem>
            <MenuRadioItem checked={pref === 'dark'} onSelect={() => setPref('dark')} icon={<Moon size={14} />}>
              Dark
            </MenuRadioItem>
            <MenuSeparator />
            <MenuItem icon={<LogOut size={14} />} onSelect={() => void logout()}>
              Sign out
            </MenuItem>
            <div className="px-2 pt-1 pb-0.5 text-2xs text-fg-faint">tgp {versionLabel(meta.data?.version, meta.data?.commit)}</div>
          </Menu>
        )}
      </div>
      {isAdmin && <CreateRepoDialog open={creating} onClose={() => setCreating(false)} />}
      <RepoChooser intent={choose} onClose={() => setChoose(null)} />
    </nav>
    </>
  )
}
