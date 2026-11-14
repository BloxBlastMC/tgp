import { useQuery } from '@tanstack/react-query'
import { ChevronDown, GitBranch, Tag } from 'lucide-react'
import { useState } from 'react'
import { branchesQuery, tagsQuery } from '../../api/queries'
import { refDisplay } from '../../lib/repoUrls'
import { Badge, FloatingPanel, SegmentedControl, SelectList, usePopover, type SelectItem } from '../ui'

/** Dropdown to switch between branches and tags, with search and keyboard support. */
export function RefPicker({
  repo,
  current,
  onSelect,
  label = 'Switch branch or tag',
  prefix,
}: {
  repo: string
  current: string
  onSelect: (ref: string) => void
  label?: string
  /** Text before the ref on the trigger, e.g. "base:". */
  prefix?: string
}) {
  const p = usePopover()
  const [tab, setTab] = useState<'branches' | 'tags'>('branches')
  const branches = useQuery({ ...branchesQuery(repo), enabled: p.open })
  const tags = useQuery({ ...tagsQuery(repo), enabled: p.open })
  const list = (tab === 'branches' ? branches.data : tags.data) ?? []
  const items: SelectItem[] = list.map((r) => ({
    key: r.name,
    label: r.name,
    render: (
      <span className="flex min-w-0 flex-1 items-center gap-2">
        {tab === 'tags' ? <Tag size={12} className="shrink-0 text-fg-faint" /> : <GitBranch size={12} className="shrink-0 text-fg-faint" />}
        <span className="min-w-0 flex-1 truncate font-mono text-sm">{r.name}</span>
        {'is_default' in r && r.is_default && <Badge>default</Badge>}
      </span>
    ),
  }))
  const pending = tab === 'branches' ? branches.isPending : tags.isPending
  return (
    <>
      <button
        type="button"
        {...p.triggerProps('listbox')}
        aria-label={`${label}: ${current}`}
        className="flex h-7 max-w-64 shrink-0 items-center gap-1.5 rounded-md border border-border-strong bg-surface-2 px-2 shadow-card transition-colors hover:bg-bg-hover"
      >
        <GitBranch size={13} className="shrink-0 text-fg-muted" />
        {prefix && <span className="text-sm text-fg-faint">{prefix}</span>}
        <span className="truncate font-mono text-sm">{refDisplay(current)}</span>
        <ChevronDown size={13} className="shrink-0 text-fg-faint" />
      </button>
      <FloatingPanel open={p.open} onClose={p.close} anchor={p.anchor} id={p.id} className="w-80 max-w-[calc(100vw-16px)] overflow-hidden">
        <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2">
          <span className="text-sm font-semibold">{label}</span>
          <SegmentedControl
            size="sm"
            label="Ref type"
            value={tab}
            onChange={setTab}
            options={[
              { value: 'branches', label: 'Branches' },
              { value: 'tags', label: 'Tags' },
            ]}
          />
        </div>
        <SelectList
          key={tab}
          items={items}
          selected={[current]}
          label={tab === 'branches' ? 'Branches' : 'Tags'}
          placeholder={tab === 'branches' ? 'Find a branch…' : 'Find a tag…'}
          emptyText={pending ? 'Loading…' : tab === 'branches' ? 'No branches found' : 'No tags yet'}
          onPick={(ref) => {
            p.setOpen(false)
            p.anchor.current?.focus()
            onSelect(ref)
          }}
        />
      </FloatingPanel>
    </>
  )
}
