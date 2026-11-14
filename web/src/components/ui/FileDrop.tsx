import { UploadCloud } from 'lucide-react'
import { useId, useRef, useState, type ReactNode } from 'react'
import { cx } from './cx'

/** A drop zone with a file picker; highlights while dragging over. */
export function FileDrop({
  onFiles,
  accept,
  multiple,
  label,
  hint,
  disabled,
  className,
  children,
  compact,
}: {
  onFiles: (files: File[]) => void
  accept?: string
  multiple?: boolean
  /** Accessible name of the file input. */
  label: string
  hint?: ReactNode
  disabled?: boolean
  className?: string
  children?: ReactNode
  compact?: boolean
}) {
  const id = useId()
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  return (
    <label
      htmlFor={id}
      onDragOver={(e) => {
        if (disabled) return
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault()
        setOver(false)
        if (disabled) return
        const files = Array.from(e.dataTransfer.files)
        if (files.length) onFiles(multiple ? files : files.slice(0, 1))
      }}
      className={cx(
        'flex cursor-pointer items-center gap-3 rounded-lg border border-dashed text-fg-muted transition-colors',
        compact ? 'px-3 py-2.5' : 'flex-col justify-center px-4 py-6 text-center',
        over ? 'border-accent bg-accent-subtle text-fg' : 'border-border-strong bg-bg-subtle hover:border-fg-faint hover:text-fg',
        disabled && 'pointer-events-none opacity-50',
        className,
      )}
    >
      <UploadCloud size={compact ? 16 : 20} className={over ? 'text-accent' : 'text-fg-faint'} />
      <span className={compact ? 'text-left' : ''}>
        {children ?? (
          <>
            <span className="font-medium text-fg">Drop {multiple ? 'files' : 'a file'} here</span> or <span className="text-accent">browse</span>
          </>
        )}
        {hint && <span className="block text-sm text-fg-faint">{hint}</span>}
      </span>
      <input
        ref={input}
        id={id}
        type="file"
        accept={accept}
        multiple={multiple}
        disabled={disabled}
        aria-label={label}
        className="sr-only"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? [])
          if (files.length) onFiles(files)
          e.target.value = ''
        }}
      />
    </label>
  )
}
