import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Check, CheckCircle2, CircleDot, CircleSlash, Milestone as MilestoneIcon, Settings2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { IssueSummary, Label, MilestoneRef, UserSummary } from '../../api/client'
import { labelsQuery, milestonesQuery, usersQuery } from '../../api/queries'
import { labelTextColor } from '../../lib/issues'
import { Avatar } from '../pulls/common'
import { cx, Input } from '../ui'

export function LabelChip({ label, onClick, title }: { label: Pick<Label, 'name' | 'color'> & { description?: string }; onClick?: () => void; title?: string }) {
  const style = { backgroundColor: `#${label.color}`, color: labelTextColor(label.color) }
  const cls = 'inline-flex h-[18px] max-w-48 items-center truncate rounded-full px-2 text-[11px] leading-none font-medium'
  if (onClick)
    return (
      <button type="button" onClick={onClick} className={cx(cls, 'hover:brightness-110')} style={style} title={title ?? label.description}>
        {label.name}
      </button>
    )
  return (
    <span className={cls} style={style} title={title ?? label.description} data-testid="label">
      {label.name}
    </span>
  )
}

export function IssueStateIcon({ issue, size = 15 }: { issue: Pick<IssueSummary, 'state' | 'state_reason'>; size?: number }) {
  if (issue.state === 'open') return <CircleDot size={size} className="shrink-0 text-success" aria-label="open" />
  if (issue.state_reason === 'not_planned') return <CircleSlash size={size} className="shrink-0 text-fg-faint" aria-label="closed as not planned" />
  return <CheckCircle2 size={size} className="shrink-0 text-[#a371f7]" aria-label="closed as completed" />
}

export function IssueStateBadge({ issue }: { issue: Pick<IssueSummary, 'state' | 'state_reason'> }) {
  const label = issue.state === 'open' ? 'Open' : issue.state_reason === 'not_planned' ? 'Closed as not planned' : 'Closed'
  const tone = issue.state === 'open' ? 'bg-success/15 text-success' : issue.state_reason === 'not_planned' ? 'bg-bg-hover text-fg-muted' : 'bg-[#a371f7]/15 text-[#a371f7]'
  return (
    <span className={cx('inline-flex h-6 items-center gap-1.5 rounded-full px-2.5 text-[12px] font-medium', tone)}>
      <IssueStateIcon issue={issue} size={13} /> {label}
    </span>
  )
}

export function MilestoneLink({ repo, milestone }: { repo: string; milestone: MilestoneRef }) {
  return (
    <Link to="/$repo/issues" params={{ repo }} search={{ milestone: milestone.title }} className="inline-flex items-center gap-1 hover:text-accent">
      <MilestoneIcon size={12} /> {milestone.title}
    </Link>
  )
}

type Item = { key: string; label: string; render: ReactNode; search: string }

/**
 * Popover list with search. Multi-select calls onChange when it closes;
 * single-select calls it on pick.
 */
function Picker({
  title,
  items,
  value,
  multi,
  onChange,
  disabled,
  emptyText,
  footer,
  trigger,
}: {
  trigger?: ReactNode
  title: string
  items: Item[]
  value: string[]
  multi: boolean
  onChange: (keys: string[]) => void
  disabled?: boolean
  emptyText: string
  footer?: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [filter, setFilter] = useState('')
  const [draft, setDraft] = useState<string[]>(value)
  const ref = useRef<HTMLDivElement>(null)
  const draftRef = useRef(draft)
  draftRef.current = draft
  const close = () => {
    setOpen(false)
    setFilter('')
    if (!multi) return
    const next = draftRef.current
    if (next.length !== value.length || next.some((k) => !value.includes(k))) onChange(next)
  }
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && close()
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  })
  const list = useMemo(() => items.filter((i) => i.search.toLowerCase().includes(filter.toLowerCase())), [items, filter])
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
        className={
          trigger
            ? 'inline-flex h-7 items-center gap-1 rounded border border-border-strong px-2 text-[12px] text-fg-muted hover:bg-bg-hover hover:text-fg'
            : 'rounded p-1 text-fg-muted hover:bg-bg-hover hover:text-fg disabled:opacity-40'
        }
        aria-label={trigger ? `Filter by ${title.toLowerCase()}` : `Edit ${title.toLowerCase()}`}
        title={trigger ? undefined : `Edit ${title.toLowerCase()}`}
      >
        {trigger ?? <Settings2 size={14} />}
      </button>
      {open && (
        <div className="absolute top-7 right-0 z-30 w-64 rounded-md border border-border-strong bg-bg-elevated shadow-xl" onKeyDown={(e) => e.key === 'Escape' && close()}>
          <div className="border-b border-border p-2">
            <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" autoFocus className="h-7" aria-label={`Filter ${title.toLowerCase()}`} />
          </div>
          <ul className="max-h-64 overflow-auto p-1" role="listbox" aria-label={title} aria-multiselectable={multi}>
            {list.length === 0 && <li className="px-3 py-3 text-center text-fg-faint">{emptyText}</li>}
            {list.map((it) => {
              const on = draft.includes(it.key)
              return (
                <li key={it.key}>
                  <button
                    type="button"
                    role="option"
                    aria-selected={on}
                    aria-label={it.label}
                    onClick={() => {
                      if (multi) setDraft(on ? draft.filter((x) => x !== it.key) : [...draft, it.key])
                      else {
                        setOpen(false)
                        setFilter('')
                        onChange(on ? [] : [it.key])
                      }
                    }}
                    className="flex min-h-8 w-full items-center gap-2 rounded px-2 py-1 text-left hover:bg-bg-hover"
                  >
                    <span className="w-3.5 shrink-0">{on && <Check size={13} />}</span>
                    {it.render}
                  </button>
                </li>
              )
            })}
          </ul>
          <div className="flex items-center justify-between border-t border-border p-1.5">
            <span className="px-1 text-[11px]">{footer}</span>
            <button type="button" className="rounded px-2 py-1 text-[12px] text-accent hover:bg-bg-hover" onClick={close}>
              Done
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

/** Picks label names. */
export function LabelPicker({
  repo,
  value,
  onChange,
  disabled,
  trigger,
}: {
  repo: string
  value: string[]
  onChange: (names: string[]) => void
  disabled?: boolean
  trigger?: ReactNode
}) {
  const labels = useQuery(labelsQuery(repo))
  const items: Item[] = (labels.data ?? []).map((l) => ({
    key: l.name,
    label: l.name,
    search: l.name + ' ' + l.description,
    render: (
      <span className="flex min-w-0 flex-1 items-start gap-2">
        <span className="mt-0.5 size-3 shrink-0 rounded-full" style={{ backgroundColor: `#${l.color}` }} />
        <span className="min-w-0">
          <span className="block truncate">{l.name}</span>
          {l.description && <span className="block truncate text-[11px] text-fg-faint">{l.description}</span>}
        </span>
      </span>
    ),
  }))
  return (
    <Picker
      title="Labels"
      trigger={trigger}
      items={items}
      value={value}
      multi
      onChange={onChange}
      disabled={disabled}
      emptyText="No labels"
      footer={
        <Link to="/$repo/labels" params={{ repo }} className="text-fg-muted hover:text-fg">
          Manage labels
        </Link>
      }
    />
  )
}

/** Picks usernames to assign. */
export function AssigneePicker({ value, onChange, disabled }: { value: string[]; onChange: (usernames: string[]) => void; disabled?: boolean }) {
  const users = useQuery(usersQuery)
  const items: Item[] = (users.data ?? []).map((u) => ({
    key: u.username,
    label: u.display_name,
    search: u.username + ' ' + u.display_name,
    render: (
      <>
        <Avatar user={u} size={18} />
        <span className="flex-1 truncate">{u.display_name}</span>
        <span className="text-[11px] text-fg-faint">{u.username}</span>
      </>
    ),
  }))
  return <Picker title="Assignees" items={items} value={value} multi onChange={onChange} disabled={disabled} emptyText="Nobody found" />
}

/** Picks one open milestone (id as string); [] clears. */
export function MilestonePicker({ repo, value, onChange, disabled }: { repo: string; value?: number; onChange: (id: number | null) => void; disabled?: boolean }) {
  const ms = useQuery(milestonesQuery(repo, 'open'))
  const items: Item[] = (ms.data ?? []).map((m) => ({
    key: String(m.id),
    label: m.title,
    search: m.title,
    render: (
      <span className="min-w-0 flex-1">
        <span className="block truncate">{m.title}</span>
        {m.due_on && <span className="block text-[11px] text-fg-faint">Due {m.due_on}</span>}
      </span>
    ),
  }))
  return (
    <Picker
      title="Milestone"
      items={items}
      value={value ? [String(value)] : []}
      multi={false}
      onChange={(keys) => onChange(keys[0] ? Number(keys[0]) : null)}
      disabled={disabled}
      emptyText="No open milestones"
      footer={
        <Link to="/$repo/milestones" params={{ repo }} className="text-fg-muted hover:text-fg">
          Manage milestones
        </Link>
      }
    />
  )
}

function Section({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="border-b border-border pb-3">
      <div className="mb-1.5 flex items-center justify-between text-[12px] font-medium text-fg-muted">
        {title}
        {action}
      </div>
      {children}
    </section>
  )
}

export type MetaPatch = { labels?: string[]; assignees?: string[]; milestone?: number }

/**
 * Assignees / Labels / Milestone sidebar sections shared by issues and pull
 * requests. onPatch receives replacement lists (milestone 0 clears).
 */
export function MetaSections({
  repo,
  labels,
  assignees,
  milestone,
  canEdit,
  onPatch,
  me,
}: {
  repo: string
  labels: Label[]
  assignees: UserSummary[]
  milestone?: MilestoneRef | null
  canEdit: boolean
  onPatch: (p: MetaPatch) => void
  me?: string
}) {
  return (
    <>
      <Section title="Assignees" action={canEdit && <AssigneePicker value={assignees.map((u) => u.username)} onChange={(a) => onPatch({ assignees: a })} />}>
        {assignees.length === 0 ? (
          <p className="text-[12px] text-fg-faint">
            No one
            {canEdit && me && (
              <>
                {' — '}
                <button type="button" className="hover:text-accent" onClick={() => onPatch({ assignees: [me] })}>
                  assign yourself
                </button>
              </>
            )}
          </p>
        ) : (
          <ul className="flex flex-col gap-1.5" aria-label="Assignees">
            {assignees.map((u) => (
              <li key={u.id} className="flex items-center gap-2">
                <Avatar user={u} size={18} /> <span className="truncate">{u.display_name}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>
      <Section title="Labels" action={canEdit && <LabelPicker repo={repo} value={labels.map((l) => l.name)} onChange={(l) => onPatch({ labels: l })} />}>
        {labels.length === 0 ? (
          <p className="text-[12px] text-fg-faint">None yet</p>
        ) : (
          <div className="flex flex-wrap gap-1" aria-label="Labels">
            {labels.map((l) => (
              <LabelChip key={l.id} label={l} />
            ))}
          </div>
        )}
      </Section>
      <Section title="Milestone" action={canEdit && <MilestonePicker repo={repo} value={milestone?.id} onChange={(id) => onPatch({ milestone: id ?? 0 })} />}>
        {milestone ? (
          <p className="text-[12px] text-fg-muted">
            <MilestoneLink repo={repo} milestone={milestone} />
          </p>
        ) : (
          <p className="text-[12px] text-fg-faint">No milestone</p>
        )}
      </Section>
    </>
  )
}
