import { ChevronDown, Search, X } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Kbd } from './Badge'
import { Button } from './Button'
import { cx } from './cx'
import { Input } from './Form'
import { Menu, MenuRadioItem } from './Menu'
import { Picker, type SelectItem } from './Combobox'
import { isTypingTarget } from './overlay'

/** The filter/search strip above a list or table. */
export function FilterBar({ children, end, className }: { children: ReactNode; end?: ReactNode; className?: string }) {
  return (
    <div className={cx('flex flex-wrap items-center gap-2 border-b border-border px-4 py-2', className)}>
      {children}
      {end && <div className="ml-auto flex items-center gap-2">{end}</div>}
    </div>
  )
}

/**
 * Search field with an icon and clear button. With `debounce`, onChange
 * fires after typing pauses. `shortcut` makes "/" focus it.
 */
export function SearchInput({
  value,
  onChange,
  placeholder = 'Search…',
  label,
  debounce = 0,
  className,
  shortcut,
  autoFocus,
  size = 'sm',
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  label: string
  debounce?: number
  className?: string
  shortcut?: boolean
  autoFocus?: boolean
  size?: 'sm' | 'md'
}) {
  const [text, setText] = useState(value)
  const ref = useRef<HTMLInputElement>(null)
  const timer = useRef<number | undefined>(undefined)
  const last = useRef(value)
  // Follow outside changes (e.g. "Clear filters") without fighting typing.
  useEffect(() => {
    if (value !== last.current) {
      last.current = value
      setText(value)
    }
  }, [value])
  useEffect(() => () => window.clearTimeout(timer.current), [])
  useEffect(() => {
    if (!shortcut) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === '/' && !isTypingTarget(e.target) && !e.metaKey && !e.ctrlKey && !document.querySelector('dialog[open]')) {
        e.preventDefault()
        ref.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [shortcut])
  const emit = (v: string, now = false) => {
    window.clearTimeout(timer.current)
    const fire = () => {
      last.current = v
      onChange(v)
    }
    if (debounce && !now) timer.current = window.setTimeout(fire, debounce)
    else fire()
  }
  return (
    <div className={cx('relative w-full max-w-xs', className)}>
      <Input
        ref={ref}
        type="search"
        size={size}
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          emit(e.target.value)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') emit(text, true)
          if (e.key === 'Escape' && text) {
            e.preventDefault()
            setText('')
            emit('', true)
          }
        }}
        placeholder={placeholder}
        aria-label={label}
        autoFocus={autoFocus}
        leading={<Search size={13} />}
        className={cx('pr-7 [&::-webkit-search-cancel-button]:hidden')}
      />
      <span className="absolute top-1/2 right-1.5 flex -translate-y-1/2 items-center">
        {text ? (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => {
              setText('')
              emit('', true)
              ref.current?.focus()
            }}
            className="rounded p-0.5 text-fg-faint hover:bg-bg-hover hover:text-fg"
          >
            <X size={12} />
          </button>
        ) : (
          shortcut && <Kbd className="pointer-events-none">/</Kbd>
        )}
      </span>
    </div>
  )
}

/** A dropdown that picks one value; the trigger shows the current choice. */
export function FilterMenu<T extends string>({
  label,
  value,
  options,
  onChange,
  icon,
}: {
  label: string
  value: T
  options: { value: T; label: string }[]
  onChange: (v: T) => void
  icon?: ReactNode
}) {
  const current = options.find((o) => o.value === value)
  return (
    <Menu
      label={label}
      trigger={(t) => (
        <Button {...t} size="sm" icon={icon} iconRight={<ChevronDown size={12} className="text-fg-faint" />} aria-label={`${label}: ${current?.label ?? value}`}>
          <span className="text-fg-muted">{label}:</span> {current?.label ?? value}
        </Button>
      )}
    >
      {options.map((o) => (
        <MenuRadioItem key={o.value} checked={o.value === value} onSelect={() => onChange(o.value)}>
          {o.label}
        </MenuRadioItem>
      ))}
    </Menu>
  )
}

/** Floating bar pinned to the bottom of a settings form; highlights unsaved changes. */
export function SaveBar({
  dirty,
  saving,
  onDiscard,
  saveLabel = 'Save changes',
  form,
  onSave,
  message,
  disabled,
}: {
  disabled?: boolean
  dirty: boolean
  saving?: boolean
  onDiscard?: () => void
  saveLabel?: string
  /** id of the form to submit. */
  form?: string
  onSave?: () => void
  message?: ReactNode
}) {
  return (
    <div
      className={cx(
        'flex items-center gap-3 rounded-lg border px-4 py-2.5 transition-colors',
        dirty || saving ? 'sticky bottom-4 z-10 shadow-popover' : 'shadow-card',
        dirty ? 'border-accent/50 bg-[color-mix(in_oklab,var(--accent)_8%,var(--surface-2))]' : 'border-border bg-surface-2',
      )}
    >
      <span className={cx('flex-1 text-sm', dirty ? 'text-fg' : 'text-fg-faint')}>{message ?? (dirty ? 'You have unsaved changes.' : 'All changes saved.')}</span>
      {onDiscard && (
        <Button variant="ghost" disabled={!dirty || saving} onClick={onDiscard}>
          Discard
        </Button>
      )}
      <Button type={form || !onSave ? 'submit' : 'button'} form={form} variant="primary" loading={saving} disabled={!dirty || disabled} onClick={onSave}>
        {saveLabel}
      </Button>
    </div>
  )
}

/** A single-choice filter as a searchable popover; the trigger shows the choice. */
export function FilterPicker({ title, icon, value, items, onChange }: { title: string; icon: ReactNode; value?: string; items: SelectItem[]; onChange: (v: string | undefined) => void }) {
  const current = items.find((i) => i.key === value)
  return (
    <Picker
      title={`Filter by ${title.toLowerCase()}`}
      items={items}
      value={value ? [value] : []}
      onChange={(keys) => onChange(keys[0])}
      placement="bottom-start"
      width="w-64"
      trigger={(t) => (
        <Button {...t} size="sm" icon={icon} iconRight={<ChevronDown size={12} className="text-fg-faint" />} aria-label={`${title}: ${current?.label ?? 'any'}`} className={value ? 'border-accent/50 text-fg' : ''}>
          {current ? (
            <>
              <span className="text-fg-muted">{title}:</span> <span className="max-w-28 truncate">{current.label}</span>
            </>
          ) : (
            <>
              <span className="text-fg-muted">{title}:</span> Any
            </>
          )}
        </Button>
      )}
    />
  )
}
