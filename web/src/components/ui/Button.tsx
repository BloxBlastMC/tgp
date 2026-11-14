import { createLink } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import type { AnchorHTMLAttributes, ButtonHTMLAttributes, ReactNode } from 'react'
import { cx } from './cx'
import { Tooltip } from './Tooltip'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-solid' | 'link'
export type ButtonSize = 'xs' | 'sm' | 'md' | 'lg'

const variants: Record<ButtonVariant, string> = {
  primary:
    'border-transparent bg-accent-solid text-accent-fg shadow-[inset_0_1px_0_rgb(255_255_255/0.12),0_1px_2px_rgb(0_0_0/0.15)] hover:bg-accent-hover',
  secondary: 'border-border-strong bg-surface-2 text-fg shadow-card hover:bg-bg-hover',
  ghost: 'border-transparent bg-transparent text-fg-muted hover:bg-bg-hover hover:text-fg',
  danger: 'border-danger/35 bg-transparent text-danger hover:border-danger/60 hover:bg-danger/10',
  'danger-solid': 'border-transparent bg-danger text-white shadow-[inset_0_1px_0_rgb(255_255_255/0.12)] hover:brightness-110',
  link: 'border-transparent bg-transparent px-0! text-accent hover:underline',
}

const sizes: Record<ButtonSize, string> = {
  xs: 'h-6 gap-1 px-1.5 text-xs',
  sm: 'h-6 gap-1 px-2 text-sm',
  md: 'h-7 gap-1.5 px-2.5 text-base',
  lg: 'h-8 gap-2 px-3.5 text-base',
}

export function buttonClass({ variant = 'secondary', size = 'md', className }: { variant?: ButtonVariant; size?: ButtonSize; className?: string } = {}) {
  return cx(
    'inline-flex shrink-0 cursor-pointer items-center justify-center rounded-md border font-medium whitespace-nowrap select-none transition-[background-color,border-color,color,filter,box-shadow] duration-100 disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50 [&_svg]:shrink-0',
    sizes[size],
    variants[variant],
    className,
  )
}

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
  /** Leading icon; replaced by a spinner while loading. */
  icon?: ReactNode
  iconRight?: ReactNode
  ref?: React.Ref<HTMLButtonElement>
}

export function Button({ variant = 'secondary', size = 'md', loading, className, children, disabled, icon, iconRight, ...rest }: ButtonProps) {
  return (
    <button type="button" {...rest} disabled={disabled || loading} aria-busy={loading || undefined} className={buttonClass({ variant, size, className })}>
      {loading ? <Loader2 size={13} className="animate-spin" /> : icon}
      {children}
      {iconRight}
    </button>
  )
}

type AnchorProps = AnchorHTMLAttributes<HTMLAnchorElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
  icon?: ReactNode
  ref?: React.Ref<HTMLAnchorElement>
}

function ButtonAnchor({ variant, size, className, icon, children, ...rest }: AnchorProps) {
  return (
    <a {...rest} className={buttonClass({ variant, size, className })}>
      {icon}
      {children}
    </a>
  )
}

/** A router link that looks like a button (never nest <Button> in <Link>). */
export const ButtonLink = createLink(ButtonAnchor)

/** A plain <a> (downloads, external) that looks like a button. */
export function ButtonA(props: AnchorProps) {
  return <ButtonAnchor {...props} />
}

const iconSizes: Record<ButtonSize, string> = { xs: 'size-5', sm: 'size-6', md: 'size-7', lg: 'size-8' }

/** Square icon-only button. `label` is its accessible name and tooltip. */
export function IconButton({
  label,
  children,
  size = 'md',
  variant = 'ghost',
  className,
  tooltip = true,
  pressed,
  loading,
  disabled,
  ...rest
}: Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'aria-label'> & {
  label: string
  size?: ButtonSize
  variant?: ButtonVariant
  tooltip?: boolean | ReactNode
  pressed?: boolean
  loading?: boolean
  ref?: React.Ref<HTMLButtonElement>
}) {
  const btn = (
    <button
      type="button"
      {...rest}
      disabled={disabled || loading}
      aria-label={label}
      aria-pressed={pressed}
      className={cx(buttonClass({ variant, size }), iconSizes[size], 'px-0', pressed && 'bg-bg-active text-fg', className)}
    >
      {loading ? <Loader2 size={13} className="animate-spin" /> : children}
    </button>
  )
  if (!tooltip) return btn
  return (
    <Tooltip content={tooltip === true ? label : tooltip} describe={tooltip !== true}>
      {btn}
    </Tooltip>
  )
}
