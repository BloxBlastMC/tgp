import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { FolderGit2, GitPullRequest, CircleDot, Play, Shield, Moon, Sun } from 'lucide-react'
import type { ComponentType } from 'react'
import { metaQuery } from '../api/queries'
import { useTheme } from '../lib/theme'

type NavItem = { to: string; label: string; icon: ComponentType<{ size?: number }>; shortcut: string }

const nav: NavItem[] = [
  { to: '/', label: 'Repositories', icon: FolderGit2, shortcut: 'G R' },
  { to: '/pulls', label: 'Pull requests', icon: GitPullRequest, shortcut: 'G P' },
  { to: '/issues', label: 'Issues', icon: CircleDot, shortcut: 'G I' },
  { to: '/ci', label: 'CI', icon: Play, shortcut: 'G C' },
  { to: '/admin', label: 'Admin', icon: Shield, shortcut: 'G A' },
]

export function Sidebar() {
  const meta = useQuery(metaQuery)
  const [theme, toggleTheme] = useTheme()
  const orgName = meta.data?.org_name || 'tgp'

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-border bg-bg-subtle">
      <div className="flex h-11 items-center gap-2 border-b border-border px-3">
        <img src="/favicon.svg" alt="" className="size-5 rounded" />
        <span className="truncate font-semibold">{orgName}</span>
      </div>
      <nav className="flex flex-1 flex-col gap-0.5 p-2">
        {nav.map(({ to, label, icon: Icon, shortcut }) => (
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
      <div className="flex items-center justify-between border-t border-border px-3 py-2 text-[11px] text-fg-faint">
        <span>tgp {meta.data?.version ?? ''}</span>
        <button
          type="button"
          onClick={toggleTheme}
          className="rounded p-1 hover:bg-bg-hover hover:text-fg"
          aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
          title="Toggle theme"
        >
          {theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />}
        </button>
      </div>
    </aside>
  )
}
