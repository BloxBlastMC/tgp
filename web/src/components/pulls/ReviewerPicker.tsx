import { useQuery } from '@tanstack/react-query'
import { Check, Settings2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { usersQuery } from '../../api/queries'
import { Input } from '../ui'
import { Avatar } from './common'

/**
 * Popover to pick reviewers by username. `exclude` hides people who can't
 * review (the author). Calls onChange when the popover closes.
 */
export function ReviewerPicker({
  value,
  exclude,
  onChange,
  disabled,
}: {
  value: string[]
  exclude: string[]
  onChange: (usernames: string[]) => void
  disabled?: boolean
}) {
  const users = useQuery(usersQuery)
  const [open, setOpen] = useState(false)
  const [filter, setFilter] = useState('')
  const [draft, setDraft] = useState<string[]>(value)
  const ref = useRef<HTMLDivElement>(null)
  const draftRef = useRef(draft)
  draftRef.current = draft

  const close = () => {
    setOpen(false)
    const next = draftRef.current
    if (next.length !== value.length || next.some((u) => !value.includes(u))) onChange(next)
  }
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && close()
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  })

  const list = useMemo(() => {
    const f = filter.toLowerCase()
    return (users.data ?? []).filter((u) => !exclude.includes(u.username) && (u.username.includes(f) || u.display_name.toLowerCase().includes(f)))
  }, [users.data, exclude, filter])

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        disabled={disabled}
        onClick={() => {
          if (open) close()
          else {
            setDraft(value)
            setOpen(true)
          }
        }}
        className="rounded p-1 text-fg-muted hover:bg-bg-hover hover:text-fg disabled:opacity-40"
        aria-label="Edit reviewers"
        title="Edit reviewers"
      >
        <Settings2 size={14} />
      </button>
      {open && (
        <div className="absolute top-7 right-0 z-30 w-64 rounded-md border border-border-strong bg-bg-elevated shadow-xl" onKeyDown={(e) => e.key === 'Escape' && close()}>
          <div className="border-b border-border p-2">
            <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Find people" autoFocus className="h-7" aria-label="Find reviewers" />
          </div>
          <ul className="max-h-64 overflow-auto p-1" role="listbox" aria-label="Reviewers" aria-multiselectable>
            {list.length === 0 && <li className="px-3 py-3 text-center text-fg-faint">Nobody found</li>}
            {list.map((u) => {
              const on = draft.includes(u.username)
              return (
                <li key={u.id}>
                  <button
                    type="button"
                    role="option"
                    aria-selected={on}
                    onClick={() => setDraft(on ? draft.filter((x) => x !== u.username) : [...draft, u.username])}
                    className="flex h-8 w-full items-center gap-2 rounded px-2 text-left hover:bg-bg-hover"
                  >
                    <span className="w-3.5">{on && <Check size={13} />}</span>
                    <Avatar user={u} size={18} />
                    <span className="flex-1 truncate">{u.display_name}</span>
                    <span className="text-[11px] text-fg-faint">{u.role}</span>
                  </button>
                </li>
              )
            })}
          </ul>
          <div className="flex justify-end border-t border-border p-1.5">
            <button type="button" className="rounded px-2 py-1 text-[12px] text-accent hover:bg-bg-hover" onClick={close}>
              Done
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
