import { Link } from '@tanstack/react-router'
import { ChevronDown, Download, Terminal } from 'lucide-react'
import { Button, CopyButton, FloatingPanel, usePopover } from '../ui'

function CopyRow({ label, value, icon }: { label: string; value: string; icon?: React.ReactNode }) {
  return (
    <div>
      <p className="mb-1.5 flex items-center gap-1.5 text-xs font-semibold tracking-wide text-fg-faint uppercase">
        {icon}
        {label}
      </p>
      <div className="flex items-center gap-1 rounded-md border border-border-strong bg-bg py-0.5 pr-0.5 pl-2.5">
        <span className="min-w-0 flex-1 truncate font-mono text-sm select-all">{value}</span>
        <CopyButton value={value} label={`Copy ${label.toLowerCase()}`} />
      </div>
    </div>
  )
}

export function CloneButton({ url, name }: { url: string; name: string }) {
  const p = usePopover()
  return (
    <>
      <Button {...p.triggerProps('dialog')} variant="primary" icon={<Download size={13} />} iconRight={<ChevronDown size={13} className="opacity-70" />}>
        Clone
      </Button>
      <FloatingPanel open={p.open} onClose={p.close} anchor={p.anchor} placement="bottom-end" id={p.id} role="dialog" label="Clone" className="flex w-[400px] max-w-[calc(100vw-16px)] flex-col gap-3 p-3.5">
        <CopyRow label="HTTPS clone URL" value={url} />
        <CopyRow label="tgp CLI" value={`tgp repo clone ${name}`} icon={<Terminal size={11} />} />
        <p className="text-sm text-fg-muted">
          Sign in with your username and a{' '}
          <Link to="/settings" hash="tokens" className="text-accent hover:underline" onClick={p.close}>
            personal access token
          </Link>{' '}
          as the password.
        </p>
      </FloatingPanel>
    </>
  )
}
