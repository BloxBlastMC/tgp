import { Check, ChevronDown } from 'lucide-react'
import { createContext, useContext, useEffect, useRef, type ReactElement, type ReactNode } from 'react'
import type { Placement } from '../../lib/position'
import { buttonClass, type ButtonVariant } from './Button'
import { cx } from './cx'
import { FloatingPanel, usePopover, type TriggerProps } from './Popover'

const MenuCtx = createContext<{ close: () => void } | null>(null)

const ITEM = '[role^="menuitem"]:not([aria-disabled="true"])'

function items(panel: HTMLElement | null): HTMLElement[] {
  return panel ? Array.from(panel.querySelectorAll<HTMLElement>(ITEM)) : []
}

/**
 * Dropdown menu (role=menu). Arrow keys, Home/End and typeahead move focus;
 * Enter/Space activate; Escape or Tab close.
 */
export function Menu({
  trigger,
  children,
  placement = 'bottom-start',
  className,
  label,
}: {
  trigger: (props: TriggerProps & { onKeyDown: (e: React.KeyboardEvent) => void }) => ReactElement
  children: ReactNode
  placement?: Placement
  className?: string
  label?: string
}) {
  const p = usePopover()
  const panel = useRef<HTMLDivElement>(null)
  const typed = useRef({ text: '', at: 0 })
  const focusFirst = useRef<'first' | 'last'>('first')

  useEffect(() => {
    if (!p.open) return
    const raf = requestAnimationFrame(() => {
      const list = items(panel.current)
      const checked = list.find((el) => el.getAttribute('aria-checked') === 'true')
      ;(checked ?? (focusFirst.current === 'last' ? list[list.length - 1] : list[0]))?.focus()
    })
    return () => cancelAnimationFrame(raf)
  }, [p.open])

  const onPanelKey = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const list = items(panel.current)
    const i = list.indexOf(document.activeElement as HTMLElement)
    const move = (n: number) => {
      e.preventDefault()
      list[(n + list.length) % list.length]?.focus()
    }
    if (e.key === 'ArrowDown') move(i + 1)
    else if (e.key === 'ArrowUp') move(i - 1)
    else if (e.key === 'Home') move(0)
    else if (e.key === 'End') move(list.length - 1)
    else if (e.key === 'Tab') p.close()
    else if (e.key.length === 1 && /\S/.test(e.key) && !e.metaKey && !e.ctrlKey) {
      const now = Date.now()
      typed.current = { text: (now - typed.current.at < 600 ? typed.current.text : '') + e.key.toLowerCase(), at: now }
      const hit = list.find((el, j) => j !== i && el.textContent?.trim().toLowerCase().startsWith(typed.current.text))
      hit?.focus()
    }
  }

  const tp = p.triggerProps('menu')
  return (
    <MenuCtx.Provider value={{ close: p.close }}>
      {trigger({
        ...tp,
        'aria-controls': p.open ? p.id : undefined,
        onKeyDown: (e) => {
          if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
            e.preventDefault()
            focusFirst.current = e.key === 'ArrowUp' ? 'last' : 'first'
            p.setOpen(true)
          }
        },
        onClick: () => {
          focusFirst.current = 'first'
          p.setOpen((o) => !o)
        },
      })}
      <FloatingPanel
        open={p.open}
        onClose={p.close}
        anchor={p.anchor}
        placement={placement}
        id={p.id}
        role="menu"
        label={label}
        panelRef={panel}
        onKeyDown={onPanelKey}
        className={cx('min-w-44 p-1', className)}
      >
        {children}
      </FloatingPanel>
    </MenuCtx.Provider>
  )
}

const itemClass =
  'flex min-h-7 w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-left text-base outline-none select-none focus:bg-bg-hover aria-disabled:cursor-not-allowed aria-disabled:opacity-50 [&_svg]:shrink-0'

export function MenuItem({
  children,
  onSelect,
  icon,
  tone,
  disabled,
  shortcut,
  description,
}: {
  children: ReactNode
  onSelect: () => void
  icon?: ReactNode
  tone?: 'danger'
  disabled?: boolean
  shortcut?: ReactNode
  description?: ReactNode
}) {
  const ctx = useContext(MenuCtx)
  return (
    <div
      role="menuitem"
      tabIndex={-1}
      aria-disabled={disabled || undefined}
      onClick={() => {
        if (disabled) return
        ctx?.close()
        onSelect()
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          e.currentTarget.click()
        }
      }}
      onMouseMove={(e) => e.currentTarget.focus()}
      className={cx(itemClass, description && 'items-start py-1.5', tone === 'danger' ? 'text-danger focus:bg-danger/10' : 'text-fg')}
    >
      {icon && <span className={cx('flex w-4 justify-center', tone !== 'danger' && 'text-fg-muted', description && 'mt-0.5')}>{icon}</span>}
      <span className="min-w-0 flex-1">
        <span className="block truncate">{children}</span>
        {description && <span className="block text-sm text-fg-muted">{description}</span>}
      </span>
      {shortcut && <span className="ml-4 text-xs text-fg-faint">{shortcut}</span>}
    </div>
  )
}

export function MenuRadioItem({ children, checked, onSelect, icon }: { children: ReactNode; checked: boolean; onSelect: () => void; icon?: ReactNode }) {
  const ctx = useContext(MenuCtx)
  return (
    <div
      role="menuitemradio"
      aria-checked={checked}
      tabIndex={-1}
      onClick={() => {
        ctx?.close()
        onSelect()
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          e.currentTarget.click()
        }
      }}
      onMouseMove={(e) => e.currentTarget.focus()}
      className={cx(itemClass, 'text-fg')}
    >
      <span className="flex w-4 justify-center text-accent">{checked && <Check size={13} />}</span>
      {icon && <span className="flex w-4 justify-center text-fg-muted">{icon}</span>}
      <span className="min-w-0 flex-1 truncate">{children}</span>
    </div>
  )
}

export function MenuSeparator() {
  return <div role="separator" className="-mx-1 my-1 h-px bg-border" />
}

export function MenuLabel({ children }: { children: ReactNode }) {
  return <div className="px-2 pt-1.5 pb-1 text-xs font-medium text-fg-faint">{children}</div>
}

/**
 * A primary action with a chevron menu of alternatives, joined as one
 * control.
 */
export function SplitButton({
  children,
  onClick,
  menuLabel,
  menu,
  variant = 'secondary',
  loading,
  disabled,
  icon,
}: {
  children: ReactNode
  onClick: () => void
  menuLabel: string
  menu: ReactNode
  variant?: ButtonVariant
  loading?: boolean
  disabled?: boolean
  icon?: ReactNode
}) {
  return (
    <div className="inline-flex">
      <button type="button" onClick={onClick} disabled={disabled || loading} className={buttonClass({ variant, className: 'rounded-r-none' })}>
        {icon}
        {children}
      </button>
      <Menu
        placement="bottom-end"
        trigger={(t) => (
          <button type="button" {...t} disabled={disabled || loading} aria-label={menuLabel} className={buttonClass({ variant, className: '-ml-px rounded-l-none px-1.5' })}>
            <ChevronDown size={14} />
          </button>
        )}
      >
        {menu}
      </Menu>
    </div>
  )
}
