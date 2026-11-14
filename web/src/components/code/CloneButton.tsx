import { Link } from '@tanstack/react-router'
import { Check, ChevronDown, Copy, Download } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Button, useCopy } from '../ui'

export function CloneButton({ url, name }: { url: string; name: string }) {
  const [open, setOpen] = useState(false)
  const [copied, copy] = useCopy()
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])
  return (
    <div className="relative" ref={ref}>
      <Button variant="primary" onClick={() => setOpen(!open)} aria-expanded={open}>
        <Download size={13} /> Clone <ChevronDown size={13} />
      </Button>
      {open && (
        <div className="absolute top-8 right-0 z-30 w-96 rounded-md border border-border-strong bg-bg-elevated p-3 shadow-xl">
          <p className="mb-1.5 text-sm font-medium text-fg-muted">Clone over HTTPS</p>
          <div className="flex items-center gap-1 rounded border border-border-strong bg-bg px-2 py-1">
            <code className="min-w-0 flex-1 truncate font-mono text-sm select-all">{url}</code>
            <button type="button" onClick={() => copy(url)} className="rounded p-1 text-fg-muted hover:bg-bg-hover hover:text-fg" aria-label="Copy clone URL">
              {copied ? <Check size={13} className="text-success" /> : <Copy size={13} />}
            </button>
          </div>
          <p className="mt-2 text-sm text-fg-faint">
            Use your username and a{' '}
            <Link to="/settings" className="text-accent hover:underline">
              personal access token
            </Link>{' '}
            as the password. With the tgp CLI: <code className="font-mono">tgp repo clone {name}</code>
          </p>
        </div>
      )}
    </div>
  )
}
