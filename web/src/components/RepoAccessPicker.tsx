import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { reposQuery } from '../api/queries'
import { Badge, Button, SearchInput } from './ui'

/** Checklist of repositories for granting access. */
export function RepoAccessPicker({ selected, onChange }: { selected: string[]; onChange: (names: string[]) => void }) {
  const repos = useQuery(reposQuery)
  const [filter, setFilter] = useState('')
  const set = useMemo(() => new Set(selected.map((s) => s.toLowerCase())), [selected])
  const shown = (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(filter.toLowerCase()))
  if (repos.data?.length === 0) return <p className="text-sm text-fg-faint">No repositories exist yet.</p>
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <SearchInput value={filter} onChange={setFilter} label="Filter repositories" placeholder="Filter repositories…" className="max-w-none flex-1" />
        <Button size="sm" variant="ghost" onClick={() => onChange((repos.data ?? []).map((r) => r.name))}>
          All
        </Button>
        <Button size="sm" variant="ghost" onClick={() => onChange([])}>
          None
        </Button>
      </div>
      <ul className="max-h-48 overflow-auto rounded-md border border-border bg-bg">
        {shown.map((r) => {
          const on = set.has(r.name.toLowerCase())
          return (
            <li key={r.id} className="border-b border-border last:border-0">
              <label className="flex cursor-pointer items-center gap-2.5 px-3 py-1.5 hover:bg-bg-hover">
                <input
                  type="checkbox"
                  checked={on}
                  onChange={(e) => onChange(e.target.checked ? [...selected, r.name] : selected.filter((s) => s.toLowerCase() !== r.name.toLowerCase()))}
                  className="size-3.5 cursor-pointer accent-[var(--accent-solid)]"
                />
                <span className="flex-1">{r.name}</span>
                {r.archived && <Badge>archived</Badge>}
              </label>
            </li>
          )
        })}
      </ul>
      <p className="text-sm text-fg-faint tabular-nums">{selected.length} selected</p>
    </div>
  )
}
