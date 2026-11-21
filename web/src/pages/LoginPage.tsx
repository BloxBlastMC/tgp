import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useState } from 'react'
import { api, ApiRequestError, ensureCsrf, errorMessage, unwrap } from '../api/client'
import { AuthCard } from '../components/AuthCard'
import { Alert, Button, Field, Input } from '../components/ui'

export function LoginPage() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { next } = useSearch({ strict: false }) as { next?: string }
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [step, setStep] = useState<'password' | 'totp' | 'recovery'>('password')
  const [code, setCode] = useState('')

  const login = useMutation({
    mutationFn: async () => {
      await ensureCsrf()
      return unwrap(
        api.POST('/auth/login', {
          body: {
            username,
            password,
            ...(step === 'totp' ? { totp_code: code.trim() } : {}),
            ...(step === 'recovery' ? { recovery_code: code.trim() } : {}),
          },
        }),
      )
    },
    onSuccess: (session) => {
      qc.setQueryData(['session'], session)
      if (session.must_change_password) {
        void navigate({ to: '/change-password' })
      } else {
        // Only follow same-origin relative paths. URL parsers read "/\host"
        // as "//host" and drop tabs and newlines ("/\t/host"), so refuse
        // both.
        const dest = next && /^\/(?![/\\])[^\x00-\x1f\x7f]*$/.test(next) ? next : '/'
        void navigate({ to: dest })
      }
    },
    onError: (e) => {
      if (e instanceof ApiRequestError && e.code === 'totp_required' && step === 'password') {
        setStep('totp')
        login.reset()
      }
    },
  })

  const showError = login.isError && !(login.error instanceof ApiRequestError && login.error.code === 'totp_required' && step === 'password')

  return (
    <AuthCard
      aside={step === 'password' ? 'New here? Use the temporary password your admin gave you.' : undefined}
      title={step === 'password' ? 'Sign in' : 'Two-factor authentication'}
      subtitle={
        step === 'totp'
          ? 'Enter the 6-digit code from your authenticator app.'
          : step === 'recovery'
            ? 'Enter one of your recovery codes.'
            : undefined
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          login.mutate()
        }}
      >
        {step === 'password' ? (
          <>
            <Field label="Username or email">
              {(id) => <Input id={id} size="lg" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus required />}
            </Field>
            <Field label="Password">
              {(id) => (
                <Input id={id} type="password" size="lg" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
              )}
            </Field>
          </>
        ) : (
          <Field label={step === 'totp' ? 'Authentication code' : 'Recovery code'}>
            {(id) => (
              <Input
                id={id}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                autoComplete="one-time-code"
                inputMode={step === 'totp' ? 'numeric' : 'text'}
                placeholder={step === 'totp' ? '123456' : 'xxxx-xxxx-xxxx'}
                size="lg"
                className="text-center font-mono text-lg tracking-[0.3em]"
                autoFocus
                required
              />
            )}
          </Field>
        )}
        {showError && <Alert>{errorMessage(login.error)}</Alert>}
        <Button type="submit" variant="primary" size="lg" loading={login.isPending} className="mt-1 w-full">
          {step === 'password' ? 'Sign in' : 'Verify'}
        </Button>
        {step !== 'password' && (
          <div className="flex justify-between text-sm">
            <button
              type="button"
              className="font-medium text-accent hover:underline"
              onClick={() => {
                setStep(step === 'totp' ? 'recovery' : 'totp')
                setCode('')
                login.reset()
              }}
            >
              {step === 'totp' ? 'Use a recovery code' : 'Use authenticator code'}
            </button>
            <button
              type="button"
              className="text-fg-muted hover:text-fg"
              onClick={() => {
                setStep('password')
                setCode('')
                login.reset()
              }}
            >
              Back
            </button>
          </div>
        )}
      </form>
    </AuthCard>
  )
}
