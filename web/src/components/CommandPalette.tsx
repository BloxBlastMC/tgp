import {
  Archive,
  CircleDot,
  CornerDownLeft,
  FolderGit2,
  Home,
  Keyboard,
  LogOut,
  Milestone,
  Package,
  Palette,
  Play,
  Plus,
  Search,
  Settings,
  Shield,
  Tag,
  Webhook,
  GitPullRequest,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type ComponentType } from 'react'
import { groupOrder, type Command, type CommandIcon } from '../lib/commands'
import { filterCommands } from '../lib/palette'
import { cx, isTypingTarget, Kbd } from './ui'

const icons: Record<CommandIcon, ComponentType<{ size?: number; className?: string }>> = {
  home: Home,
  repo: FolderGit2,
  pull: GitPullRequest,
  issue: CircleDot,
  ci: Play,
  settings: Settings,
  logout: LogOut,
  theme: Palette,
  admin: Shield,
  release: Package,
  label: Tag,
  milestone: Milestone,
  webhook: Webhook,
  cache: Archive,
  plus: Plus,
  keyboard: Keyboard,
}

/**
 * ⌘K / Ctrl+K command palette plus two-key "g x" navigation shortcuts and
 * "?" for the shortcuts dialog.
 */
export function CommandPalette({
  commands,
  open,
  setOpen,
  onHelp,
}: {
  commands: Command[]
  open: boolean
  setOpen: (v: boolean | ((o: boolean) => boolean)) => void
  onHelp: () => void
}) {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const dialog = useRef<HTMLDialogElement>(null)
  const list = useRef<HTMLUListElement>(null)

  useEffect(() => {
    let pendingG = 0
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen((o) => !o)
        return
      }
      if (isTypingTarget(e.target) || e.metaKey || e.ctrlKey || e.altKey || document.querySelector('dialog[open]')) return
      if (e.key === '?') {
        e.preventDefault()
        onHelp()
        return
      }
      if (e.key === 'g') {
        pendingG = window.setTimeout(() => (pendingG = 0), 800)
        return
      }
      if (pendingG) {
        window.clearTimeout(pendingG)
        pendingG = 0
        const cmd = commands.find((c) => c.shortcut === `g ${e.key.toLowerCase()}`)
        if (cmd) {
          e.preventDefault()
          cmd.run()
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [commands, setOpen, onHelp])

  useEffect(() => {
    const d = dialog.current
    if (!d) return
    if (open && !d.open) {
      setQuery('')
      setActive(0)
      d.showModal()
    } else if (!open && d.open) d.close()
  }, [open])

  // No query: grouped in a fixed order. With a query: best match first.
  const filtered = useMemo(() => {
    if (query.trim()) return filterCommands(commands, query)
    return groupOrder.flatMap((g) => commands.filter((c) => c.group === g).slice(0, g === 'Repositories' ? 8 : undefined))
  }, [commands, query])
  const grouped = !query.trim()

  useEffect(() => {
    list.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [active])

  const run = (c: Command | undefined) => {
    if (!c) return
    setOpen(false)
    c.run()
  }

  let lastGroup = ''
  return (
    <dialog
      ref={dialog}
      onClose={() => setOpen(false)}
      onClick={(e) => e.target === dialog.current && setOpen(false)}
      className="mx-auto mt-[12vh] w-[calc(100%-32px)] max-w-xl overflow-hidden rounded-xl border border-border-strong bg-surface-2 p-0 text-fg shadow-dialog open:animate-pop-in"
      aria-label="Command palette"
    >
      <div className="flex items-center gap-2.5 border-b border-border px-4">
        <Search size={15} className="text-fg-faint" />
        <input
          autoFocus
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setActive(0)
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setActive((a) => Math.min(a + 1, filtered.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setActive((a) => Math.max(a - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              run(filtered[active])
            }
          }}
          placeholder="Search pages, repositories and actions…"
          className="h-12 flex-1 bg-transparent text-md outline-none placeholder:text-fg-faint focus-visible:outline-none"
          aria-label="Command"
        />
        <Kbd>Esc</Kbd>
      </div>
      <ul ref={list} className="max-h-[min(420px,60vh)] overflow-auto p-1.5" role="listbox" aria-label="Commands">
        {filtered.length === 0 && <li className="px-3 py-10 text-center text-fg-faint">No results for “{query}”</li>}
        {filtered.map((c, i) => {
          const Icon = icons[c.icon ?? 'repo']
          const header = grouped && c.group !== lastGroup ? c.group : null
          lastGroup = c.group
          return (
            <li key={c.id} role="presentation">
              {header && <div className="px-2.5 pt-2.5 pb-1 text-xs font-medium text-fg-faint">{header}</div>}
              <div
                role="option"
                data-index={i}
                aria-selected={i === active}
                onMouseMove={() => setActive(i)}
                onClick={() => run(c)}
                className={cx('flex h-9 cursor-pointer items-center gap-2.5 rounded-lg px-2.5', i === active && 'bg-bg-hover')}
              >
                <Icon size={15} className={i === active ? 'text-fg' : 'text-fg-faint'} />
                <span className="flex-1 truncate">{c.label}</span>
                {!grouped && <span className="text-xs text-fg-faint">{c.group}</span>}
                {c.shortcut && (
                  <span className="flex gap-0.5">
                    {c.shortcut.split(' ').map((k) => (
                      <Kbd key={k}>{k.toUpperCase()}</Kbd>
                    ))}
                  </span>
                )}
                {i === active && !c.shortcut && <CornerDownLeft size={13} className="text-fg-faint" />}
              </div>
            </li>
          )
        })}
      </ul>
      <div className="flex items-center gap-4 border-t border-border bg-bg-subtle px-4 py-2 text-xs text-fg-faint">
        <span className="flex items-center gap-1">
          <Kbd>↑</Kbd>
          <Kbd>↓</Kbd> to navigate
        </span>
        <span className="flex items-center gap-1">
          <Kbd>↵</Kbd> to open
        </span>
        <span className="flex-1" />
        <span className="flex items-center gap-1">
          <Kbd>?</Kbd> shortcuts
        </span>
      </div>
    </dialog>
  )
}

export const shortcutGroups: { title: string; items: { keys: string[]; label: string }[] }[] = [
  {
    title: 'General',
    items: [
      { keys: ['Ctrl', 'K'], label: 'Open the command palette' },
      { keys: ['?'], label: 'Show keyboard shortcuts' },
      { keys: ['/'], label: 'Focus the search field' },
      { keys: ['Esc'], label: 'Close a dialog or menu' },
    ],
  },
  {
    title: 'Go to',
    items: [
      { keys: ['G', 'H'], label: 'Home' },
      { keys: ['G', 'R'], label: 'Repositories' },
      { keys: ['G', 'P'], label: 'Pull requests' },
      { keys: ['G', 'I'], label: 'Issues' },
      { keys: ['G', 'C'], label: 'CI runs' },
      { keys: ['G', 'S'], label: 'Settings' },
    ],
  },
  {
    title: 'Writing',
    items: [{ keys: ['Ctrl', 'Enter'], label: 'Submit a comment' }],
  },
]
