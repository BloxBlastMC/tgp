import { useQuery } from '@tanstack/react-query'
import { Check, ChevronDown, GitBranch, Tag } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { branchesQuery, tagsQuery } from '../../api/queries'
import { cx, Input } from '../ui'

/** Dropdown to switch between branches and tags. */
export function RefPicker({ repo, current, onSelect }: { repo: string; current: string; onSelect: (ref: string) => void }) {
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState<'branches' | 'tags'>('branches')
  const [filter, setFilter] = useState('')
  const branches = useQuery({ ...branchesQuery(repo), enabled: open })
  const tags = useQuery({ ...tagsQuery(repo), enabled: open })
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const items = useMemo(() => {
    const list = (tab === 'branches' ? branches.data : tags.data) ?? []
    const f = filter.toLowerCase()
    return list.filter((r) => r.name.toLowerCase().includes(f))
  }, [tab, branches.data, tags.data, filter])

  const short = /^[0-9a-f]{40}$/.test(current) ? current.slice(0, 7) : current
  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex h-7 max-w-56 items-center gap-1.5 rounded border border-border-strong bg-bg-elevated px-2 hover:bg-bg-hover"
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <GitBranch size={13} className="shrink-0 text-fg-muted" />
        <span className="truncate font-mono text-[12px]">{short}</span>
        <ChevronDown size={13} className="shrink-0 text-fg-muted" />
      </button>
      {open && (
        <div className="absolute top-8 left-0 z-30 w-72 rounded-md border border-border-strong bg-bg-elevated shadow-xl">
          <div className="border-b border-border p-2">
            <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Find a branch or tag" autoFocus className="h-7" />
          </div>
          <div className="flex border-b border-border text-[12px]">
            {(['branches', 'tags'] as const).map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => setTab(t)}
                className={cx('flex-1 py-1.5 capitalize', tab === t ? 'border-b-2 border-accent text-fg' : 'text-fg-muted hover:text-fg')}
              >
                {t}
              </button>
            ))}
          </div>
          <ul className="max-h-72 overflow-auto p-1" role="listbox">
            {items.length === 0 && <li className="px-3 py-4 text-center text-fg-faint">Nothing found</li>}
            {items.map((r) => (
              <li key={r.name}>
                <button
                  type="button"
                  role="option"
                  aria-selected={r.name === current}
                  onClick={() => {
                    setOpen(false)
                    onSelect(r.name)
                  }}
                  className="flex h-7 w-full items-center gap-2 rounded px-2 text-left hover:bg-bg-hover"
                >
                  <span className="w-3.5">{r.name === current && <Check size={13} />}</span>
                  {tab === 'tags' && <Tag size={12} className="text-fg-faint" />}
                  <span className="flex-1 truncate font-mono text-[12px]">{r.name}</span>
                  {r.is_default && <span className="text-[10px] text-fg-faint">default</span>}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
