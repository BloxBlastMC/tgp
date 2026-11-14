import { Check, Search } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react'
import { filterItems } from '../../lib/filter'
import type { Placement } from '../../lib/position'
import { cx } from './cx'
import { FloatingPanel, usePopover, type TriggerProps } from './Popover'

export type SelectItem = {
  key: string
  /** Accessible name and default rendering. */
  label: string
  /** Extra text to match on. */
  search?: string
  render?: ReactNode
  group?: string
  disabled?: boolean
}

/**
 * Filterable listbox driven from a text input (combobox pattern): typing
 * filters, arrows move the active option, Enter picks it.
 */
export function SelectList({
  items,
  selected,
  onPick,
  label,
  placeholder = 'Filter…',
  emptyText = 'No matches',
  multi,
  autoFocus = true,
  header,
  maxHeight = 'max-h-72',
}: {
  items: SelectItem[]
  selected: string[]
  onPick: (key: string) => void
  label: string
  placeholder?: string
  emptyText?: ReactNode
  multi?: boolean
  autoFocus?: boolean
  header?: ReactNode
  maxHeight?: string
}) {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const listId = useId()
  const list = useRef<HTMLUListElement>(null)
  const shown = useMemo(() => filterItems(items, query, (i) => `${i.label} ${i.search ?? ''}`), [items, query])
  useEffect(() => {
    list.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [active])
  const optId = (i: number) => `${listId}-${i}`
  let lastGroup: string | undefined

  return (
    <div className="flex min-w-0 flex-col">
      {header}
      <div className="flex items-center gap-2 border-b border-border px-2.5">
        <Search size={13} className="shrink-0 text-fg-faint" />
        <input
          role="combobox"
          aria-expanded
          aria-controls={listId}
          aria-activedescendant={shown.length ? optId(active) : undefined}
          aria-autocomplete="list"
          aria-label={`Filter ${label.toLowerCase()}`}
          autoFocus={autoFocus}
          value={query}
          placeholder={placeholder}
          onChange={(e) => {
            setQuery(e.target.value)
            setActive(0)
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setActive((a) => Math.min(a + 1, shown.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setActive((a) => Math.max(a - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              const it = shown[active]
              if (it && !it.disabled) onPick(it.key)
            }
          }}
          className="h-9 min-w-0 flex-1 bg-transparent text-base outline-none placeholder:text-fg-faint focus-visible:outline-none"
        />
      </div>
      <ul ref={list} id={listId} role="listbox" aria-label={label} aria-multiselectable={multi || undefined} className={cx('overflow-auto p-1', maxHeight)}>
        {shown.length === 0 && <li className="px-3 py-4 text-center text-sm text-fg-faint">{emptyText}</li>}
        {shown.map((it, i) => {
          const on = selected.includes(it.key)
          const groupHeader = it.group && it.group !== lastGroup ? it.group : null
          lastGroup = it.group
          return (
            <li key={it.key} role="presentation">
              {groupHeader && (
                <div role="presentation" className="px-2 pt-2 pb-1 text-xs font-medium text-fg-faint">
                  {groupHeader}
                </div>
              )}
              <div
                id={optId(i)}
                data-index={i}
                role="option"
                aria-selected={on}
                aria-disabled={it.disabled || undefined}
                aria-label={it.label}
                onMouseMove={() => setActive(i)}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => !it.disabled && onPick(it.key)}
                className={cx(
                  'flex min-h-8 cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-left',
                  i === active && 'bg-bg-hover',
                  it.disabled && 'cursor-not-allowed opacity-50',
                )}
              >
                <span className="flex w-3.5 shrink-0 justify-center text-accent">{on && <Check size={13} />}</span>
                {it.render ?? <span className="min-w-0 flex-1 truncate">{it.label}</span>}
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/**
 * A popover picker over a SelectList. Multi-select collects a draft and
 * applies it when closed (Done, outside click or Escape); single-select
 * applies on pick.
 */
export function Picker({
  title,
  items,
  value,
  multi,
  onChange,
  emptyText,
  footer,
  trigger,
  placement = 'bottom-end',
  width = 'w-72',
  header,
  placeholder,
}: {
  title: string
  items: SelectItem[]
  value: string[]
  multi?: boolean
  onChange: (keys: string[]) => void
  emptyText?: ReactNode
  footer?: ReactNode
  trigger: (props: TriggerProps) => React.ReactElement
  placement?: Placement
  width?: string
  header?: ReactNode
  placeholder?: string
}) {
  const p = usePopover()
  const [draft, setDraft] = useState<string[]>(value)
  const draftRef = useRef(draft)
  draftRef.current = draft
  const close = () => {
    p.setOpen(false)
    if (!multi) return
    const next = draftRef.current
    if (next.length !== value.length || next.some((k) => !value.includes(k))) onChange(next)
  }
  const tp = p.triggerProps('listbox')
  return (
    <>
      {trigger({
        ...tp,
        onClick: () => {
          if (p.open) close()
          else {
            setDraft(value)
            p.setOpen(true)
          }
        },
      })}
      <FloatingPanel open={p.open} onClose={close} anchor={p.anchor} placement={placement} id={p.id} className={cx(width, 'max-w-[calc(100vw-16px)] overflow-hidden')}>
        <div className="flex items-center justify-between border-b border-border px-3 py-2">
          <span className="text-sm font-semibold">{title}</span>
        </div>
        <SelectList
          items={items}
          selected={multi ? draft : value}
          multi={multi}
          label={title}
          emptyText={emptyText}
          header={header}
          placeholder={placeholder}
          onPick={(key) => {
            if (multi) setDraft((d) => (d.includes(key) ? d.filter((x) => x !== key) : [...d, key]))
            else {
              p.setOpen(false)
              onChange(value.includes(key) ? [] : [key])
              p.anchor.current?.focus()
            }
          }}
        />
        {(multi || footer) && (
          <div className="flex items-center justify-between gap-2 border-t border-border bg-bg-subtle px-2 py-1.5">
            <span className="min-w-0 px-1 text-xs text-fg-muted">{footer}</span>
            {multi && (
              <button type="button" className="h-6 rounded-md px-2.5 text-sm font-medium text-accent hover:bg-bg-hover" onClick={close}>
                Done
              </button>
            )}
          </div>
        )}
      </FloatingPanel>
    </>
  )
}
