import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { reposQuery } from '../api/queries'
import { Input } from './ui'

/** Checklist of repositories for granting access. */
export function RepoAccessPicker({ selected, onChange }: { selected: string[]; onChange: (names: string[]) => void }) {
  const repos = useQuery(reposQuery)
  const [filter, setFilter] = useState('')
  const set = useMemo(() => new Set(selected.map((s) => s.toLowerCase())), [selected])
  const shown = (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(filter.toLowerCase()))
  if (repos.data?.length === 0) return <p className="text-sm text-fg-faint">No repositories exist yet.</p>
  return (
    <div className="flex flex-col gap-1.5">
      <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter repositories" className="h-7" aria-label="Filter repositories" />
      <ul className="max-h-48 overflow-auto rounded border border-border">
        {shown.map((r) => {
          const on = set.has(r.name.toLowerCase())
          return (
            <li key={r.id}>
              <label className="flex cursor-pointer items-center gap-2 px-2.5 py-1.5 hover:bg-bg-hover">
                <input
                  type="checkbox"
                  checked={on}
                  onChange={(e) => onChange(e.target.checked ? [...selected, r.name] : selected.filter((s) => s.toLowerCase() !== r.name.toLowerCase()))}
                  className="size-3.5 accent-[var(--accent)]"
                />
                <span className="flex-1">{r.name}</span>
                {r.archived && <span className="text-xs text-fg-faint">archived</span>}
              </label>
            </li>
          )
        })}
      </ul>
      <p className="text-sm text-fg-faint">{selected.length} selected</p>
    </div>
  )
}
