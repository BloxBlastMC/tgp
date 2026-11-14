import type { ReactNode } from 'react'
import { cx } from './cx'

export type Tone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info' | 'done'

export const toneText: Record<Tone, string> = {
  neutral: 'text-fg-muted',
  accent: 'text-accent',
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
  info: 'text-info',
  done: 'text-done',
}

export const toneBg: Record<Tone, string> = {
  neutral: 'bg-fg-faint',
  accent: 'bg-accent',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
  info: 'bg-info',
  done: 'bg-done',
}

const badgeTones: Record<Tone, string> = {
  neutral: 'bg-bg-hover text-fg-muted ring-border',
  accent: 'bg-accent-subtle text-accent ring-accent/20',
  success: 'bg-success/12 text-success ring-success/20',
  warning: 'bg-warning/12 text-warning ring-warning/20',
  danger: 'bg-danger/12 text-danger ring-danger/20',
  info: 'bg-info/12 text-info ring-info/20',
  done: 'bg-done/12 text-done ring-done/20',
}

export function Badge({ tone = 'neutral', children, icon, className, title }: { tone?: Tone; children: ReactNode; icon?: ReactNode; className?: string; title?: string }) {
  return (
    <span
      title={title}
      className={cx(
        'inline-flex h-[18px] shrink-0 items-center gap-1 rounded px-1.5 text-xs font-medium whitespace-nowrap ring-1 ring-inset',
        badgeTones[tone],
        className,
      )}
    >
      {icon}
      {children}
    </span>
  )
}

/** A small round count, e.g. next to a tab or nav item. */
export function Counter({ children, tone = 'neutral', className, ...rest }: { children: ReactNode; tone?: 'neutral' | 'accent'; className?: string; title?: string; 'aria-label'?: string }) {
  return (
    <span
      {...rest}
      className={cx(
        'inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-2xs font-semibold tabular-nums',
        tone === 'accent' ? 'bg-accent-solid text-accent-fg' : 'bg-bg-active text-fg-muted',
        className,
      )}
    >
      {children}
    </span>
  )
}

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cx(
        'inline-flex h-[18px] min-w-[18px] items-center justify-center rounded border border-border-strong bg-bg-subtle px-1 font-sans text-2xs font-medium text-fg-muted shadow-[0_1px_0_var(--border-strong)]',
        className,
      )}
    >
      {children}
    </kbd>
  )
}

/** A coloured status dot. */
export function Dot({ tone, pulse, className, label }: { tone: Tone; pulse?: boolean; className?: string; label?: string }) {
  return (
    <span className={cx('relative inline-flex size-2 shrink-0', className)} role={label ? 'img' : undefined} aria-label={label} aria-hidden={label ? undefined : true}>
      {pulse && <span className={cx('absolute inset-0 animate-ping rounded-full opacity-60', toneBg[tone])} />}
      <span className={cx('relative size-2 rounded-full', toneBg[tone])} />
    </span>
  )
}

/** A rounded state pill (Open / Merged / Closed …) with an icon. */
export function Pill({ tone, icon, children, size = 'md' }: { tone: Tone; icon?: ReactNode; children: ReactNode; size?: 'sm' | 'md' }) {
  const solid: Record<Tone, string> = {
    neutral: 'bg-bg-active text-fg-muted',
    accent: 'bg-accent-solid text-white',
    success: 'bg-[color-mix(in_oklab,var(--success),black_18%)] text-white',
    warning: 'bg-warning text-black',
    danger: 'bg-[color-mix(in_oklab,var(--danger),black_12%)] text-white',
    info: 'bg-info text-white',
    done: 'bg-[color-mix(in_oklab,var(--done),black_15%)] text-white',
  }
  return (
    <span
      className={cx(
        'inline-flex shrink-0 items-center gap-1.5 rounded-full font-medium whitespace-nowrap',
        size === 'md' ? 'h-6 px-2.5 text-sm' : 'h-5 px-2 text-xs',
        solid[tone],
      )}
    >
      {icon}
      {children}
    </span>
  )
}
