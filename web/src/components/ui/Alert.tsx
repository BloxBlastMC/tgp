import { AlertTriangle, CheckCircle2, Info, XCircle } from 'lucide-react'
import type { ReactNode } from 'react'
import { cx } from './cx'

type AlertTone = 'danger' | 'warning' | 'info' | 'success'

const tones: Record<AlertTone, string> = {
  danger: 'border-danger/30 bg-danger/8 [&>svg]:text-danger',
  warning: 'border-warning/30 bg-warning/8 [&>svg]:text-warning',
  info: 'border-info/30 bg-info/8 [&>svg]:text-info',
  success: 'border-success/30 bg-success/8 [&>svg]:text-success',
}

const icons: Record<AlertTone, typeof Info> = { danger: XCircle, warning: AlertTriangle, info: Info, success: CheckCircle2 }

export function Alert({
  tone = 'danger',
  title,
  children,
  action,
  className,
}: {
  tone?: AlertTone
  title?: ReactNode
  children?: ReactNode
  action?: ReactNode
  className?: string
}) {
  const Icon = icons[tone]
  return (
    <div role={tone === 'danger' ? 'alert' : 'status'} className={cx('flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-base text-fg', tones[tone], className)}>
      <Icon size={15} className="mt-0.5 shrink-0" aria-hidden />
      <div className="min-w-0 flex-1">
        {title && <p className="font-medium">{title}</p>}
        {children && <div className={cx(title ? 'mt-0.5 text-fg-muted' : '', 'min-w-0 break-words')}>{children}</div>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  )
}

/** Error alert with standard page padding, for failed queries. */
export function ErrorState({ error, className }: { error: unknown; className?: string }) {
  const msg = error instanceof Error ? error.message : String(error)
  return (
    <div className={cx('p-4', className)}>
      <Alert>{msg}</Alert>
    </div>
  )
}
