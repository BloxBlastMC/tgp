import { useMutation, useQueryClient } from '@tanstack/react-query'
import QRCode from 'qrcode'
import { useEffect, useState } from 'react'
import { api, errorMessage, unwrap } from '../api/client'
import { Alert, Button, Field, Input, SecretValue } from './ui'

/**
 * Two-factor enrollment: shows a QR code for the pending secret, confirms a
 * code, then shows the recovery codes once.
 */
export function TotpEnroll({ onDone, onCancel }: { onDone: () => void; onCancel?: () => void }) {
  const qc = useQueryClient()
  const start = useMutation({ mutationFn: () => unwrap(api.POST('/account/totp')) })
  const [qr, setQr] = useState('')
  const [code, setCode] = useState('')
  const enable = useMutation({
    mutationFn: () => unwrap(api.POST('/account/totp/enable', { body: { code: code.trim() } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['session'] }),
  })

  const { mutate: startMutate } = start
  useEffect(() => startMutate(), [startMutate])
  useEffect(() => {
    if (start.data) {
      QRCode.toDataURL(start.data.otpauth_url, { margin: 1, width: 176, errorCorrectionLevel: 'M' }).then(setQr, () => setQr(''))
    }
  }, [start.data])

  if (enable.data) {
    return <RecoveryCodes codes={enable.data.codes} onDone={onDone} />
  }
  if (start.isError) return <Alert>{errorMessage(start.error)}</Alert>
  if (!start.data) return <p className="text-fg-muted">Generating secret…</p>

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        enable.mutate()
      }}
    >
      <ol className="flex list-decimal flex-col gap-1 pl-4 text-fg-muted">
        <li>Scan the QR code with an authenticator app (1Password, Authy, Google Authenticator…).</li>
        <li>Enter the 6-digit code it shows.</li>
      </ol>
      <div className="flex items-center gap-4">
        <div className="rounded bg-white p-1.5">{qr ? <img src={qr} alt="2FA QR code" className="size-[176px]" /> : <div className="size-[176px]" />}</div>
        <div className="min-w-0 flex-1">
          <p className="mb-1 text-sm text-fg-muted">Can't scan? Enter this key manually:</p>
          <SecretValue value={start.data.secret} label="TOTP secret" />
        </div>
      </div>
      <Field label="Authentication code">
        {(id) => (
          <Input
            id={id}
            value={code}
            onChange={(e) => setCode(e.target.value)}
            inputMode="numeric"
            autoComplete="one-time-code"
            placeholder="123456"
            maxLength={7}
            autoFocus
            className="font-mono tracking-widest"
          />
        )}
      </Field>
      {enable.isError && <Alert>{errorMessage(enable.error)}</Alert>}
      <div className="flex justify-end gap-2">
        {onCancel && (
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        )}
        <Button type="submit" variant="primary" loading={enable.isPending} disabled={code.trim().length < 6}>
          Turn on 2FA
        </Button>
      </div>
    </form>
  )
}

export function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [saved, setSaved] = useState(false)
  const download = () => {
    const blob = new Blob([codes.join('\n') + '\n'], { type: 'text/plain' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'tgp-recovery-codes.txt'
    a.click()
    URL.revokeObjectURL(a.href)
    setSaved(true)
  }
  return (
    <div className="flex flex-col gap-4">
      <Alert tone="success">Two-factor authentication is on.</Alert>
      <p className="text-fg-muted">
        Save these recovery codes somewhere safe. Each one logs you in once if you lose your authenticator. They won't be shown again.
      </p>
      <div className="grid grid-cols-2 gap-x-6 gap-y-1 rounded border border-border-strong bg-bg p-3 font-mono text-base">
        {codes.map((c) => (
          <span key={c}>{c}</span>
        ))}
      </div>
      <div className="flex justify-between gap-2">
        <div className="flex gap-2">
          <Button onClick={download}>Download</Button>
          <Button
            onClick={() => {
              void navigator.clipboard?.writeText(codes.join('\n'))
              setSaved(true)
            }}
          >
            Copy
          </Button>
        </div>
        <Button variant="primary" onClick={onDone}>
          {saved ? 'Done' : "I've saved them"}
        </Button>
      </div>
    </div>
  )
}
