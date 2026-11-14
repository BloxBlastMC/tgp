import { Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { Command } from '../lib/commands'
import { cx } from './ui'
import { filterCommands } from '../lib/palette'

function isTyping(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null
  return !!t && (t.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName))
}

/**
 * ⌘K / Ctrl+K command palette plus two-key "g x" navigation shortcuts.
 */
export function CommandPalette({
  commands,
  open,
  setOpen,
}: {
  commands: Command[]
  open: boolean
  setOpen: (v: boolean | ((o: boolean) => boolean)) => void
}) {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const dialog = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    let pendingG = 0
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen((o) => !o)
        return
      }
      if (isTyping(e) || e.metaKey || e.ctrlKey || e.altKey || dialog.current?.open) return
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
  }, [commands, setOpen])

  useEffect(() => {
    const d = dialog.current
    if (!d) return
    if (open && !d.open) {
      setQuery('')
      setActive(0)
      d.showModal()
    } else if (!open && d.open) d.close()
  }, [open])

  const filtered = useMemo(() => filterCommands(commands, query), [commands, query])

  const run = (c: Command | undefined) => {
    if (!c) return
    setOpen(false)
    c.run()
  }

  return (
    <dialog
      ref={dialog}
      onClose={() => setOpen(false)}
      onClick={(e) => e.target === dialog.current && setOpen(false)}
      className="mx-auto mt-[15vh] w-[calc(100%-32px)] max-w-lg rounded-lg border border-border-strong bg-bg-elevated p-0 text-fg shadow-2xl backdrop:bg-black/50"
      aria-label="Command palette"
    >
      <div className="flex items-center gap-2 border-b border-border px-3">
        <Search size={14} className="text-fg-faint" />
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
          placeholder="Type a command or search…"
          className="h-10 flex-1 bg-transparent text-[14px] outline-none placeholder:text-fg-faint focus-visible:outline-none"
          aria-label="Command"
        />
      </div>
      <ul className="max-h-80 overflow-auto p-1" role="listbox">
        {filtered.length === 0 && <li className="px-3 py-6 text-center text-fg-faint">No commands</li>}
        {filtered.map((c, i) => (
          <li
            key={c.id}
            role="option"
            aria-selected={i === active}
            onMouseMove={() => setActive(i)}
            onClick={() => run(c)}
            className={cx('flex h-8 cursor-pointer items-center gap-2 rounded px-3', i === active && 'bg-bg-hover')}
          >
            <span className="w-24 shrink-0 text-[11px] text-fg-faint">{c.group}</span>
            <span className="flex-1">{c.label}</span>
            {c.shortcut && <kbd className="font-mono text-[11px] text-fg-faint">{c.shortcut}</kbd>}
          </li>
        ))}
      </ul>
    </dialog>
  )
}
