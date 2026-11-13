import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Schemas } from '../../api/client'
import { sessionQuery, tokensQuery } from '../../api/queries'
import { Page } from '../../components/Page'
import { RecoveryCodes, TotpEnroll } from '../../components/TotpEnroll'
import { Alert, Badge, Button, Checkbox, Dialog, Field, Input, SecretValue, Section, Select, Spinner, Time } from '../../components/ui'

export function SettingsPage() {
  const session = useQuery(sessionQuery)
  const user = session.data?.user
  if (!user) return <Spinner />
  return (
    <Page title="Settings">
      <div className="mx-auto flex max-w-3xl flex-col gap-5 p-6">
        <Section title="Profile" description="Your name, email and role are managed by an admin.">
          <dl className="grid grid-cols-[120px_1fr] gap-y-1.5">
            <dt className="text-fg-muted">Username</dt>
            <dd className="font-mono">{user.username}</dd>
            <dt className="text-fg-muted">Display name</dt>
            <dd>{user.display_name}</dd>
            <dt className="text-fg-muted">Email</dt>
            <dd>{user.email || <span className="text-fg-faint">none</span>}</dd>
            <dt className="text-fg-muted">Role</dt>
            <dd>
              <Badge tone="accent">{user.role}</Badge>
              <span className="ml-2 text-fg-muted">{user.all_repos ? 'all repositories' : 'selected repositories'}</span>
            </dd>
          </dl>
        </Section>
        <PasswordSection />
        <TwoFactorSection enabled={user.totp_enabled} />
        <TokensSection isAdmin={user.role === 'admin'} />
      </div>
    </Page>
  )
}

function PasswordSection() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const change = useMutation({
    mutationFn: () => unwrap(api.POST('/account/password', { body: { current_password: current, new_password: next } })),
    onSuccess: () => {
      setCurrent('')
      setNext('')
    },
  })
  return (
    <Section title="Password" description="Changing your password signs you out of your other sessions.">
      <form
        className="flex max-w-sm flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          change.mutate()
        }}
      >
        <Field label="Current password">
          {(id) => <Input id={id} type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />}
        </Field>
        <Field label="New password" hint="At least 10 characters.">
          {(id) => <Input id={id} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" minLength={10} required />}
        </Field>
        {change.isError && <Alert>{errorMessage(change.error)}</Alert>}
        {change.isSuccess && <Alert tone="success">Password changed.</Alert>}
        <Button type="submit" className="self-start" loading={change.isPending}>
          Change password
        </Button>
      </form>
    </Section>
  )
}

function TwoFactorSection({ enabled }: { enabled: boolean }) {
  const qc = useQueryClient()
  const [mode, setMode] = useState<null | 'enroll' | 'disable' | 'regenerate'>(null)
  const [password, setPassword] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)
  const close = () => {
    setMode(null)
    setPassword('')
    setCodes(null)
    disable.reset()
    regen.reset()
  }
  const disable = useMutation({
    mutationFn: () => unwrap(api.POST('/account/totp/disable', { body: { password } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['session'] })
      close()
    },
  })
  const regen = useMutation({
    mutationFn: () => unwrap(api.POST('/account/recovery-codes', { body: { password } })),
    onSuccess: (r) => setCodes(r.codes),
  })

  return (
    <Section
      title="Two-factor authentication"
      description="Require a code from an authenticator app when you sign in."
      actions={
        enabled ? (
          <>
            <Button onClick={() => setMode('regenerate')}>New recovery codes</Button>
            <Button variant="danger" onClick={() => setMode('disable')}>
              Turn off
            </Button>
          </>
        ) : (
          <Button variant="primary" onClick={() => setMode('enroll')}>
            Set up
          </Button>
        )
      }
    >
      {enabled ? <Badge tone="success">Enabled</Badge> : <Badge>Off</Badge>}

      <Dialog open={mode === 'enroll'} onClose={close} title="Set up two-factor authentication" width="max-w-lg">
        <TotpEnroll onDone={close} onCancel={close} />
      </Dialog>

      <Dialog open={mode === 'disable' || mode === 'regenerate'} onClose={close} title={mode === 'disable' ? 'Turn off 2FA' : 'New recovery codes'}>
        {codes ? (
          <RecoveryCodes codes={codes} onDone={close} />
        ) : (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault()
              if (mode === 'disable') disable.mutate()
              else regen.mutate()
            }}
          >
            <p className="text-fg-muted">
              {mode === 'disable' ? 'Your account will only be protected by your password.' : 'Your existing recovery codes will stop working.'}
            </p>
            <Field label="Confirm your password">
              {(id) => <Input id={id} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" autoFocus required />}
            </Field>
            {(disable.isError || regen.isError) && <Alert>{errorMessage(disable.error ?? regen.error)}</Alert>}
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={close}>
                Cancel
              </Button>
              <Button type="submit" variant={mode === 'disable' ? 'danger' : 'primary'} loading={disable.isPending || regen.isPending}>
                {mode === 'disable' ? 'Turn off 2FA' : 'Generate'}
              </Button>
            </div>
          </form>
        )}
      </Dialog>
    </Section>
  )
}

type Scope = Schemas['TokenScope']

function TokensSection({ isAdmin }: { isAdmin: boolean }) {
  const qc = useQueryClient()
  const tokens = useQuery(tokensQuery)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<Scope[]>(['read', 'write'])
  const [expiry, setExpiry] = useState('90')
  const [created, setCreated] = useState<string | null>(null)

  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/account/tokens', {
          body: { name, scopes, ...(expiry ? { expires_in_days: Number(expiry) } : {}) },
        }),
      ),
    onSuccess: (r) => {
      setCreated(r.token)
      void qc.invalidateQueries({ queryKey: tokensQuery.queryKey })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/account/tokens/{id}', { params: { path: { id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: tokensQuery.queryKey }),
  })
  const close = () => {
    setOpen(false)
    setCreated(null)
    setName('')
    create.reset()
  }
  const toggle = (s: Scope, on: boolean) => setScopes(on ? [...scopes, s] : scopes.filter((x) => x !== s))

  return (
    <Section
      title="Personal access tokens"
      description={
        <>
          For the <code className="font-mono">tgp</code> CLI, git over HTTPS and scripts. Use them as the password for git, or as{' '}
          <code className="font-mono">Authorization: Bearer …</code>.
        </>
      }
      actions={
        <Button onClick={() => setOpen(true)}>
          <Plus size={14} /> New token
        </Button>
      }
    >
      {tokens.isPending ? (
        <Spinner />
      ) : tokens.data?.length === 0 ? (
        <p className="text-fg-muted">No tokens yet.</p>
      ) : (
        <ul className="-my-2 divide-y divide-border">
          {tokens.data?.map((t) => (
            <li key={t.id} className="flex items-center gap-3 py-2">
              <KeyRound size={14} className="shrink-0 text-fg-faint" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{t.name}</span>
                  <code className="font-mono text-[12px] text-fg-faint">{t.prefix}…</code>
                  {t.scopes.map((s) => (
                    <Badge key={s} tone={s === 'admin' ? 'warning' : 'neutral'}>
                      {s}
                    </Badge>
                  ))}
                </div>
                <div className="text-[12px] text-fg-faint">
                  Created <Time iso={t.created_at} /> · last used <Time iso={t.last_used_at} /> ·{' '}
                  {t.expires_at ? (
                    <>
                      expires <Time iso={t.expires_at} />
                    </>
                  ) : (
                    'never expires'
                  )}
                </div>
              </div>
              <Button variant="ghost" aria-label={`Revoke ${t.name}`} onClick={() => revoke.mutate(t.id)}>
                <Trash2 size={14} />
              </Button>
            </li>
          ))}
        </ul>
      )}

      <Dialog open={open} onClose={close} title={created ? 'Token created' : 'New personal access token'}>
        {created ? (
          <>
            <Alert tone="warning">Copy this token now. It won't be shown again.</Alert>
            <SecretValue value={created} label="New token" />
            <div className="flex justify-end">
              <Button variant="primary" onClick={close}>
                Done
              </Button>
            </div>
          </>
        ) : (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault()
              create.mutate()
            }}
          >
            <Field label="Name">{(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} placeholder="laptop CLI" autoFocus required />}</Field>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 text-[12px] font-medium text-fg-muted">Scopes</legend>
              <Checkbox checked={scopes.includes('read')} onChange={(v) => toggle('read', v)} label="read" hint="Clone and view everything you have access to." />
              <Checkbox checked={scopes.includes('write')} onChange={(v) => toggle('write', v)} label="write" hint="Push, open and merge PRs, comment, manage your account." />
              {isAdmin && <Checkbox checked={scopes.includes('admin')} onChange={(v) => toggle('admin', v)} label="admin" hint="Use admin endpoints (users, org, server settings)." />}
            </fieldset>
            <Field label="Expiration">
              {(id) => (
                <Select id={id} value={expiry} onChange={(e) => setExpiry(e.target.value)}>
                  <option value="7">7 days</option>
                  <option value="30">30 days</option>
                  <option value="90">90 days</option>
                  <option value="365">1 year</option>
                  <option value="">Never</option>
                </Select>
              )}
            </Field>
            {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={close}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" loading={create.isPending} disabled={!name.trim() || scopes.length === 0}>
                Create token
              </Button>
            </div>
          </form>
        )}
      </Dialog>
    </Section>
  )
}
