import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CheckCircle2, ChevronDown, CircleDot, CircleSlash, Milestone as MilestoneIcon, Pencil } from 'lucide-react'
import type { ReactNode } from 'react'
import type { IssueSummary, Label, MilestoneRef, UserSummary } from '../../api/client'
import { labelsQuery, milestonesQuery, usersQuery } from '../../api/queries'
import { dueLabel } from '../../lib/format'
import { labelTextColor } from '../../lib/issues'
import { issueState } from '../../lib/states'
import { Avatar, Button, cx, IconButton, MetaSection, Picker as UiPicker, Pill, type SelectItem } from '../ui'

export function LabelChip({ label, onClick, title }: { label: Pick<Label, 'name' | 'color'> & { description?: string }; onClick?: () => void; title?: string }) {
  const style = { backgroundColor: `#${label.color}`, color: labelTextColor(label.color) }
  const cls = 'inline-flex h-[18px] max-w-48 items-center truncate rounded-full px-2 text-xs leading-none font-medium'
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
  return <CheckCircle2 size={size} className="shrink-0 text-done" aria-label="closed as completed" />
}

export function IssueStateBadge({ issue }: { issue: Pick<IssueSummary, 'state' | 'state_reason'> }) {
  const st = issueState(issue)
  const Icon = st.key === 'open' ? CircleDot : st.key === 'not_planned' ? CircleSlash : CheckCircle2
  return (
    <Pill tone={st.tone} icon={<Icon size={13} />}>
      {st.label}
    </Pill>
  )
}

export function MilestoneLink({ repo, milestone }: { repo: string; milestone: MilestoneRef }) {
  return (
    <Link to="/$repo/issues" params={{ repo }} search={{ milestone: milestone.title }} className="inline-flex items-center gap-1 hover:text-accent">
      <MilestoneIcon size={12} /> {milestone.title}
    </Link>
  )
}

type Item = SelectItem

/**
 * Popover list with search. Multi-select calls onChange when it closes;
 * single-select calls it on pick. Without `trigger` it's a small gear
 * button labelled "Edit <title>"; with one, a filter button.
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
  return (
    <UiPicker
      title={title}
      items={items}
      value={value}
      multi={multi}
      onChange={onChange}
      emptyText={emptyText}
      footer={footer}
      placement={trigger ? 'bottom-start' : 'bottom-end'}
      trigger={(t) =>
        trigger ? (
          <Button {...t} size="sm" disabled={disabled} aria-label={`Filter by ${title.toLowerCase()}`} iconRight={<ChevronDown size={12} className="text-fg-faint" />}>
            {trigger}
          </Button>
        ) : (
          <IconButton {...t} size="xs" disabled={disabled} label={`Edit ${title.toLowerCase()}`}>
            <Pencil size={13} />
          </IconButton>
        )
      }
    />
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
          {l.description && <span className="block truncate text-xs text-fg-faint">{l.description}</span>}
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
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <Avatar user={u} size={18} />
        <span className="flex-1 truncate">{u.display_name}</span>
        <span className="text-xs text-fg-faint">{u.username}</span>
      </span>
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
        {m.due_on && <span className="block text-xs text-fg-faint">{dueLabel(m.due_on, m.state)?.text}</span>}
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

const Section = MetaSection

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
          <p className="text-sm text-fg-faint">
            No one
            {canEdit && me && (
              <>
                {' — '}
                <button type="button" className="font-medium text-accent hover:underline" onClick={() => onPatch({ assignees: [me] })}>
                  assign yourself
                </button>
              </>
            )}
          </p>
        ) : (
          <ul className="flex flex-col gap-2" aria-label="Assignees">
            {assignees.map((u) => (
              <li key={u.id} className="flex items-center gap-2">
                <Avatar user={u} size={20} /> <span className="truncate">{u.display_name}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>
      <Section title="Labels" action={canEdit && <LabelPicker repo={repo} value={labels.map((l) => l.name)} onChange={(l) => onPatch({ labels: l })} />}>
        {labels.length === 0 ? (
          <p className="text-sm text-fg-faint">None yet</p>
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
          <p className="text-sm text-fg-muted">
            <MilestoneLink repo={repo} milestone={milestone} />
          </p>
        ) : (
          <p className="text-sm text-fg-faint">No milestone</p>
        )}
      </Section>
    </>
  )
}
