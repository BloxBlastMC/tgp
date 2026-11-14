import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap } from '../api/client'
import { sessionQuery } from '../api/queries'
import { AuthCard } from '../components/AuthCard'
import { TotpEnroll } from '../components/TotpEnroll'
import { Alert, Button, Field, Input } from '../components/ui'

/**
 * Shown after logging in with a temporary password: set a real password,
 * then optionally turn on 2FA.
 */
export function ChangePasswordPage() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const session = useQuery(sessionQuery)
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [stage, setStage] = useState<'password' | 'offer2fa' | 'enroll'>('password')

  const change = useMutation({
    mutationFn: () => unwrap(api.POST('/account/password', { body: { current_password: current, new_password: next } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['session'] })
      setStage(session.data?.user.totp_enabled ? 'password' : 'offer2fa')
      if (session.data?.user.totp_enabled) void navigate({ to: '/' })
    },
  })
  const mismatch = confirm !== '' && next !== confirm
  const user = session.data?.user

  if (stage === 'offer2fa') {
    return (
      <AuthCard title="Password set" subtitle="Add a second step to your login? You can also do this later in Settings.">
        <div className="flex flex-col gap-3">
          <Button variant="primary" size="lg" icon={<ShieldCheck size={15} />} onClick={() => setStage('enroll')}>
            Set up two-factor authentication
          </Button>
          <Button variant="ghost" size="lg" onClick={() => void navigate({ to: '/' })}>
            Skip for now
          </Button>
        </div>
      </AuthCard>
    )
  }
  if (stage === 'enroll') {
    return (
      <AuthCard title="Set up two-factor authentication" wide>
        <TotpEnroll onDone={() => void navigate({ to: '/' })} onCancel={() => void navigate({ to: '/' })} />
      </AuthCard>
    )
  }

  return (
    <AuthCard
      title={user?.must_change_password ? 'Choose your password' : 'Change password'}
      subtitle={
        user?.must_change_password ? (
          <>
            Welcome{user ? `, ${user.display_name}` : ''}. Replace your temporary password before continuing.
          </>
        ) : undefined
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          if (!mismatch) change.mutate()
        }}
      >
        <input type="text" autoComplete="username" value={user?.username ?? ''} readOnly hidden />
        <Field label={user?.must_change_password ? 'Temporary password' : 'Current password'}>
          {(id) => <Input id={id} type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" autoFocus required />}
        </Field>
        <Field label="New password" hint="At least 10 characters." error={next && next.length < 10 ? `${10 - next.length} more character${next.length === 9 ? '' : 's'} needed.` : undefined}>
          {(id) => <Input id={id} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" minLength={10} required />}
        </Field>
        <Field label="Confirm new password" error={mismatch ? "The passwords don't match." : undefined}>
          {(id) => <Input id={id} type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" required aria-invalid={mismatch || undefined} />}
        </Field>
        {change.isError && <Alert>{errorMessage(change.error)}</Alert>}
        <Button type="submit" variant="primary" size="lg" className="mt-1 w-full" loading={change.isPending} disabled={mismatch || next.length < 10}>
          Set password
        </Button>
      </form>
    </AuthCard>
  )
}
