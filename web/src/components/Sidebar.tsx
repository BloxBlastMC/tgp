import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { CircleDot, FolderGit2, GitPullRequest, LogOut, Moon, Play, Search, Settings, Shield, Sun } from 'lucide-react'
import type { ComponentType } from 'react'
import { metaQuery, sessionQuery } from '../api/queries'
import { useLogout } from '../lib/commands'

type NavItem = { to: string; label: string; icon: ComponentType<{ size?: number }>; shortcut: string; adminOnly?: boolean }

const nav: NavItem[] = [
  { to: '/', label: 'Repositories', icon: FolderGit2, shortcut: 'G R' },
  { to: '/pulls', label: 'Pull requests', icon: GitPullRequest, shortcut: 'G P' },
  { to: '/issues', label: 'Issues', icon: CircleDot, shortcut: 'G I' },
  { to: '/ci', label: 'CI', icon: Play, shortcut: 'G C' },
  { to: '/admin', label: 'Admin', icon: Shield, shortcut: 'G A', adminOnly: true },
]

export function Sidebar({ theme, toggleTheme, openPalette }: { theme: 'dark' | 'light'; toggleTheme: () => void; openPalette: () => void }) {
  const meta = useQuery(metaQuery)
  const session = useQuery(sessionQuery)
  const logout = useLogout()
  const user = session.data?.user
  const orgName = meta.data?.org_name || 'tgp'

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-border bg-bg-subtle">
      <div className="flex h-11 items-center gap-2 border-b border-border px-3">
        <img src={meta.data?.has_logo ? '/api/v1/org/logo' : '/favicon.svg'} alt="" className="size-5 rounded object-cover" />
        <span className="truncate font-semibold">{orgName}</span>
      </div>
      <div className="p-2 pb-0">
        <button
          type="button"
          onClick={openPalette}
          className="flex h-7 w-full items-center gap-2 rounded border border-border bg-bg px-2 text-fg-faint hover:border-border-strong hover:text-fg-muted"
        >
          <Search size={13} />
          <span className="flex-1 text-left">Search…</span>
          <kbd className="font-mono text-[10px]">⌘K</kbd>
        </button>
      </div>
      <nav className="flex flex-1 flex-col gap-0.5 p-2">
        {nav
          .filter((n) => !n.adminOnly || user?.role === 'admin')
          .map(({ to, label, icon: Icon, shortcut }) => (
            <Link
              key={to}
              to={to}
              activeOptions={{ exact: to === '/' }}
              className="group flex h-7 items-center gap-2 rounded px-2 text-fg-muted hover:bg-bg-hover hover:text-fg data-[status=active]:bg-bg-hover data-[status=active]:text-fg"
            >
              <Icon size={15} />
              <span className="flex-1">{label}</span>
              <kbd className="hidden font-mono text-[10px] text-fg-faint group-hover:inline">{shortcut}</kbd>
            </Link>
          ))}
      </nav>
      <div className="border-t border-border p-2">
        {user && (
          <div className="mb-1 flex items-center gap-2 px-2 py-1">
            <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent-subtle text-[11px] font-semibold text-accent uppercase">
              {user.display_name.slice(0, 1)}
            </span>
            <div className="min-w-0 flex-1 leading-tight">
              <div className="truncate font-medium">{user.display_name}</div>
              <div className="truncate text-[11px] text-fg-faint">
                {user.username} · {user.role}
              </div>
            </div>
          </div>
        )}
        <div className="flex items-center gap-0.5">
          <Link
            to="/settings"
            className="flex h-7 flex-1 items-center gap-2 rounded px-2 text-fg-muted hover:bg-bg-hover hover:text-fg data-[status=active]:bg-bg-hover data-[status=active]:text-fg"
          >
            <Settings size={14} /> Settings
          </Link>
          <button
            type="button"
            onClick={toggleTheme}
            className="rounded p-1.5 text-fg-muted hover:bg-bg-hover hover:text-fg"
            aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
            title="Toggle theme"
          >
            {theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />}
          </button>
          <button type="button" onClick={() => void logout()} className="rounded p-1.5 text-fg-muted hover:bg-bg-hover hover:text-fg" aria-label="Sign out" title="Sign out">
            <LogOut size={14} />
          </button>
        </div>
        <div className="px-2 pt-1 text-[10px] text-fg-faint">tgp {meta.data?.version ?? ''}</div>
      </div>
    </aside>
  )
}
