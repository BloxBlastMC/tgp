import { useQuery } from '@tanstack/react-query'
import { Pencil } from 'lucide-react'
import { usersQuery } from '../../api/queries'
import { Avatar, IconButton, Picker, type SelectItem } from '../ui'

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
  const items: SelectItem[] = (users.data ?? [])
    .filter((u) => !exclude.includes(u.username))
    .map((u) => ({
      key: u.username,
      label: u.display_name,
      search: u.username,
      render: (
        <span className="flex min-w-0 flex-1 items-center gap-2">
          <Avatar user={u} size={18} />
          <span className="flex-1 truncate">{u.display_name}</span>
          <span className="text-xs text-fg-faint capitalize">{u.role}</span>
        </span>
      ),
    }))
  return (
    <Picker
      title="Reviewers"
      items={items}
      value={value}
      multi
      onChange={onChange}
      emptyText="Nobody found"
      placeholder="Find people…"
      trigger={(t) => (
        <IconButton {...t} size="xs" disabled={disabled} label="Edit reviewers">
          <Pencil size={13} />
        </IconButton>
      )}
    />
  )
}
