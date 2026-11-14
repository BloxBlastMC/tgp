import { Check, Copy, Loader2, X } from 'lucide-react'
import {
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
} from 'react'

export function cx(...c: (string | false | null | undefined)[]) {
  return c.filter(Boolean).join(' ')
}

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'

const variants: Record<Variant, string> = {
  primary: 'bg-accent text-accent-fg hover:brightness-110 border-transparent',
  secondary: 'bg-bg-elevated text-fg border-border-strong hover:bg-bg-hover',
  ghost: 'bg-transparent text-fg-muted border-transparent hover:bg-bg-hover hover:text-fg',
  danger: 'bg-transparent text-danger border-danger/40 hover:bg-danger/10',
}

export function Button({
  variant = 'secondary',
  loading,
  className,
  children,
  disabled,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; loading?: boolean }) {
  return (
    <button
      type="button"
      {...rest}
      disabled={disabled || loading}
      className={cx(
        'inline-flex h-7 items-center justify-center gap-1.5 rounded border px-2.5 text-[13px] font-medium whitespace-nowrap transition-[filter,background-color] disabled:cursor-not-allowed disabled:opacity-50',
        variants[variant],
        className,
      )}
    >
      {loading && <Loader2 size={13} className="animate-spin" />}
      {children}
    </button>
  )
}

export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...rest}
      className={cx(
        'h-8 w-full rounded border border-border-strong bg-bg px-2.5 text-[13px] text-fg placeholder:text-fg-faint focus:border-accent focus:outline-none',
        className,
      )}
    />
  )
}

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...rest}
      className={cx(
        'h-8 w-full rounded border border-border-strong bg-bg px-2 text-[13px] text-fg focus:border-accent focus:outline-none',
        className,
      )}
    >
      {children}
    </select>
  )
}

/** A labelled form control. The child receives the generated id. */
export function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: ReactNode
  children: (id: string) => ReactNode
}) {
  const id = useId()
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-[12px] font-medium text-fg-muted">
        {label}
      </label>
      {children(id)}
      {hint && <p className="text-[12px] text-fg-faint">{hint}</p>}
    </div>
  )
}

export function Checkbox({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  hint?: string
}) {
  const id = useId()
  return (
    <div className="flex items-start gap-2">
      <input
        id={id}
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-0.5 size-3.5 accent-[var(--accent)]"
      />
      <label htmlFor={id} className="select-none">
        <span>{label}</span>
        {hint && <span className="block text-[12px] text-fg-faint">{hint}</span>}
      </label>
    </div>
  )
}

const badgeTones = {
  neutral: 'bg-bg-hover text-fg-muted',
  accent: 'bg-accent-subtle text-accent',
  success: 'bg-success/15 text-success',
  warning: 'bg-warning/15 text-warning',
  danger: 'bg-danger/15 text-danger',
}

export function Badge({ tone = 'neutral', children }: { tone?: keyof typeof badgeTones; children: ReactNode }) {
  return (
    <span className={cx('inline-flex h-[18px] items-center rounded px-1.5 text-[11px] font-medium', badgeTones[tone])}>
      {children}
    </span>
  )
}

export function Alert({ tone = 'danger', children }: { tone?: 'danger' | 'warning' | 'info' | 'success'; children: ReactNode }) {
  const tones = {
    danger: 'border-danger/30 bg-danger/10 text-danger',
    warning: 'border-warning/30 bg-warning/10 text-warning',
    info: 'border-info/30 bg-info/10 text-info',
    success: 'border-success/30 bg-success/10 text-success',
  }
  return (
    <div role={tone === 'danger' ? 'alert' : 'status'} className={cx('rounded border px-3 py-2 text-[13px]', tones[tone])}>
      {children}
    </div>
  )
}

/** A bordered settings section with a title row. */
export function Section({
  title,
  description,
  actions,
  children,
}: {
  title: string
  description?: ReactNode
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="rounded-md border border-border bg-bg-subtle">
      <header className="flex items-start justify-between gap-4 border-b border-border px-4 py-3">
        <div>
          <h2 className="font-medium">{title}</h2>
          {description && <p className="mt-0.5 text-[12px] text-fg-muted">{description}</p>}
        </div>
        {actions && <div className="flex shrink-0 gap-2">{actions}</div>}
      </header>
      <div className="p-4">{children}</div>
    </section>
  )
}

export function useCopy(): [boolean, (text: string) => void] {
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  return [
    copied,
    (text: string) => {
      void navigator.clipboard?.writeText(text).then(() => {
        setCopied(true)
        window.clearTimeout(timer.current)
        timer.current = window.setTimeout(() => setCopied(false), 1500)
      })
    },
  ]
}

/** A one-time secret (temp password, token) with a copy button. */
export function SecretValue({ value, label }: { value: string; label?: string }) {
  const [copied, copy] = useCopy()
  return (
    <div className="flex items-center gap-2 rounded border border-border-strong bg-bg px-2.5 py-1.5">
      <code className="min-w-0 flex-1 font-mono text-[13px] break-all select-all" aria-label={label}>
        {value}
      </code>
      <button
        type="button"
        onClick={() => copy(value)}
        className="rounded p-1 text-fg-muted hover:bg-bg-hover hover:text-fg"
        aria-label="Copy to clipboard"
        title="Copy"
      >
        {copied ? <Check size={14} className="text-success" /> : <Copy size={14} />}
      </button>
    </div>
  )
}

/** Modal dialog built on <dialog> (focus trapping and Esc for free). */
export function Dialog({
  open,
  onClose,
  title,
  children,
  footer,
  width = 'max-w-md',
}: {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  footer?: ReactNode
  width?: string
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) d.showModal()
    if (!open && d.open) d.close()
  }, [open])
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onClose={onClose}
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
      className={cx(
        'm-auto w-[calc(100%-32px)] rounded-lg border border-border-strong bg-bg-elevated p-0 text-fg shadow-2xl backdrop:bg-black/50',
        width,
      )}
    >
      {open && (
        <div className="flex flex-col">
          <header className="flex items-center justify-between border-b border-border px-4 py-2.5">
            <h2 id={titleId} className="font-medium">
              {title}
            </h2>
            <button type="button" onClick={onClose} className="rounded p-1 text-fg-muted hover:bg-bg-hover hover:text-fg" aria-label="Close">
              <X size={14} />
            </button>
          </header>
          <div className="flex flex-col gap-3 p-4">{children}</div>
          {footer && <footer className="flex justify-end gap-2 border-t border-border px-4 py-2.5">{footer}</footer>}
        </div>
      )}
    </dialog>
  )
}

export function Spinner() {
  return (
    <div className="flex justify-center p-8 text-fg-faint">
      <Loader2 className="animate-spin" size={18} />
    </div>
  )
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

export function relativeTime(iso?: string | null): string {
  if (!iso) return 'never'
  const diff = (new Date(iso).getTime() - Date.now()) / 1000
  const abs = Math.abs(diff)
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 31536000],
    ['month', 2592000],
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ]
  for (const [unit, secs] of units) {
    if (abs >= secs) return rtf.format(Math.round(diff / secs), unit)
  }
  return 'just now'
}

export function Time({ iso }: { iso?: string | null }) {
  return (
    <time dateTime={iso ?? undefined} title={iso ? new Date(iso).toLocaleString() : undefined}>
      {relativeTime(iso)}
    </time>
  )
}
