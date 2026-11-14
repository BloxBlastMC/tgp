import { Check, Copy } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ButtonSize } from './Button'
import { IconButton } from './Button'
import { cx } from './cx'

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

/** Icon button that copies `value`; the tooltip confirms. */
export function CopyButton({ value, label = 'Copy', size = 'sm', className }: { value: string; label?: string; size?: ButtonSize; className?: string }) {
  const [copied, copy] = useCopy()
  return (
    <IconButton label={label} tooltip={copied ? 'Copied!' : label} size={size} onClick={() => copy(value)} className={className}>
      {copied ? <Check size={13} className="text-success" /> : <Copy size={13} />}
    </IconButton>
  )
}

/** A value shown in a mono field with a copy button (tokens, URLs). */
export function SecretValue({ value, label, className }: { value: string; label?: string; className?: string }) {
  return (
    <div className={cx('flex items-center gap-2 rounded-md border border-border-strong bg-bg py-1 pr-1 pl-2.5', className)}>
      <code className="min-w-0 flex-1 font-mono text-code break-all select-all" aria-label={label}>
        {value}
      </code>
      <CopyButton value={value} label="Copy to clipboard" />
    </div>
  )
}
