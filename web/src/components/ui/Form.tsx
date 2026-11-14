import { useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { cx } from './cx'

const control =
  'w-full rounded-md border border-border-strong bg-bg text-fg placeholder:text-fg-faint transition-[border-color,box-shadow] duration-100 hover:border-[color-mix(in_oklab,var(--border-strong),var(--fg)_15%)] focus:border-accent focus:shadow-[0_0_0_3px_var(--accent-subtle)] focus:outline-none focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60 aria-invalid:border-danger aria-invalid:focus:shadow-[0_0_0_3px_color-mix(in_oklab,var(--danger)_18%,transparent)]'

const inputSizes = { sm: 'h-7 px-2 text-sm', md: 'h-8 px-2.5 text-base', lg: 'h-9 px-3 text-base' }

export function Input({
  className,
  size = 'md',
  leading,
  wrapperClassName,
  ...rest
}: Omit<InputHTMLAttributes<HTMLInputElement>, 'size'> & {
  size?: keyof typeof inputSizes
  /** An icon shown inside the field, before the text. */
  leading?: ReactNode
  wrapperClassName?: string
  ref?: React.Ref<HTMLInputElement>
}) {
  const input = <input {...rest} className={cx(control, inputSizes[size], leading != null && (size === 'sm' ? 'pl-7' : 'pl-8'), className)} />
  if (leading == null) return input
  return (
    <div className={cx('relative', wrapperClassName)}>
      <span className={cx('pointer-events-none absolute top-1/2 -translate-y-1/2 text-fg-faint', size === 'sm' ? 'left-2' : 'left-2.5')}>{leading}</span>
      {input}
    </div>
  )
}

export function Textarea({ className, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement> & { ref?: React.Ref<HTMLTextAreaElement> }) {
  return <textarea {...rest} className={cx(control, 'min-h-20 px-2.5 py-1.5 text-base leading-relaxed', className)} />
}

// A neutral grey chevron reads fine on both themes.
const chevron =
  "bg-[url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%23888' stroke-width='2.5' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E\")] bg-[position:right_8px_center] bg-no-repeat"

export function Select({
  className,
  children,
  size = 'md',
  ...rest
}: Omit<SelectHTMLAttributes<HTMLSelectElement>, 'size'> & { size?: keyof typeof inputSizes }) {
  return (
    <select {...rest} className={cx(control, inputSizes[size], chevron, 'cursor-pointer appearance-none pr-7', className)}>
      {children}
    </select>
  )
}

/** A labelled form control. The child receives the generated id. */
export function Field({
  label,
  hint,
  error,
  required,
  children,
  className,
}: {
  label: ReactNode
  hint?: ReactNode
  error?: ReactNode
  required?: boolean
  children: (id: string) => ReactNode
  className?: string
}) {
  const id = useId()
  return (
    <div className={cx('flex flex-col gap-1.5', className)}>
      <label htmlFor={id} className="text-sm font-medium text-fg">
        {label}
        {required && (
          <span className="ml-0.5 text-danger" aria-hidden>
            *
          </span>
        )}
      </label>
      {children(id)}
      {error ? (
        <p className="text-sm text-danger" role="alert">
          {error}
        </p>
      ) : (
        hint && <p className="text-sm text-fg-faint">{hint}</p>
      )}
    </div>
  )
}

export function Checkbox({
  checked,
  onChange,
  label,
  hint,
  disabled,
  className,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: ReactNode
  hint?: ReactNode
  disabled?: boolean
  className?: string
}) {
  const id = useId()
  return (
    <div className={cx('flex items-start gap-2.5', className)}>
      <input
        id={id}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-[3px] size-3.5 shrink-0 cursor-pointer rounded accent-[var(--accent-solid)] disabled:cursor-not-allowed"
      />
      <label htmlFor={id} className={cx('select-none', disabled ? 'text-fg-faint' : 'cursor-pointer')}>
        <span>{label}</span>
        {hint && <span className="block text-sm text-fg-faint">{hint}</span>}
      </label>
    </div>
  )
}

/** On/off toggle (role=switch). */
export function Switch({
  checked,
  onChange,
  label,
  hint,
  disabled,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: ReactNode
  hint?: ReactNode
  disabled?: boolean
}) {
  const id = useId()
  return (
    <div className="flex items-start justify-between gap-4">
      <label htmlFor={id} className="cursor-pointer select-none">
        <span className="font-medium">{label}</span>
        {hint && <span className="block text-sm text-fg-faint">{hint}</span>}
      </label>
      <button
        id={id}
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={cx(
          'relative inline-flex h-[18px] w-8 shrink-0 cursor-pointer items-center rounded-full border transition-colors duration-150 disabled:opacity-50',
          checked ? 'border-transparent bg-accent-solid' : 'border-border-strong bg-bg-active',
        )}
      >
        <span className={cx('size-3 rounded-full bg-white shadow transition-transform duration-150', checked ? 'translate-x-[15px]' : 'translate-x-[2px]')} />
      </button>
    </div>
  )
}

/** A radio card: a selectable option with a title and description. */
export function RadioCard({
  name,
  value,
  checked,
  onChange,
  title,
  description,
  icon,
}: {
  name: string
  value: string
  checked: boolean
  onChange: (v: string) => void
  title: ReactNode
  description?: ReactNode
  icon?: ReactNode
}) {
  return (
    <label
      className={cx(
        'flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors',
        checked ? 'border-accent bg-accent-subtle' : 'border-border-strong hover:bg-bg-hover',
      )}
    >
      <input type="radio" name={name} value={value} checked={checked} onChange={() => onChange(value)} className="mt-[3px] accent-[var(--accent-solid)]" />
      {icon && <span className="mt-0.5 shrink-0">{icon}</span>}
      <span className="min-w-0">
        <span className="block font-medium">{title}</span>
        {description && <span className="block text-sm text-fg-muted">{description}</span>}
      </span>
    </label>
  )
}
