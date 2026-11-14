import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Lock, Monitor, Moon, Palette, Plus, Settings as SettingsIcon, ShieldCheck, ShieldOff, Sun, Trash2, User } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, errorMessage, unwrap, type Schemas } from '../../api/client'
import { sessionQuery, tokensQuery } from '../../api/queries'
import { Page, SideNav, TwoColumn } from '../../components/Page'
import { RecoveryCodes, TotpEnroll } from '../../components/TotpEnroll'
import {
  Alert,
  Avatar,
  Badge,
  Button,
  Card,
  Checkbox,
  DescriptionList,
  Dialog,
  EmptyState,
  Field,
  IconButton,
  Input,
  PageSkeleton,
  SecretValue,
  Section,
  SegmentedControl,
  Select,
  SkeletonRows,
  Time,
  useConfirm,
  useToast,
} from '../../components/ui'
import { useTheme } from '../../lib/theme'

export function SettingsPage() {
  const session = useQuery(sessionQuery)
  const user = session.data?.user
  useEffect(() => {
    const id = window.location.hash.slice(1)
    if (id && user) document.getElementById(id)?.scrollIntoView({ block: 'start' })
  }, [user])
  if (!user) return <PageSkeleton rows={3} />
  return (
    <Page title="Settings" icon={SettingsIcon}>
      <TwoColumn
        className="max-w-5xl"
        nav={
          <SideNav
            label="Account sections"
            groups={[
              {
                items: [
                  { to: '/settings', label: 'Profile', icon: User },
                  { to: '/settings', label: 'Appearance', icon: Palette, hash: 'appearance' },
                  { to: '/settings', label: 'Password', icon: Lock, hash: 'password' },
                  { to: '/settings', label: 'Two-factor', icon: ShieldCheck, hash: 'two-factor' },
                  { to: '/settings', label: 'Access tokens', icon: KeyRound, hash: 'tokens' },
                ],
              },
            ]}
          />
        }
      >
        <div className="flex flex-col gap-6">
          <Card id="profile" title="Profile" description="Your name, email and role are managed by an admin.">
            <div className="flex items-center gap-4">
              <Avatar user={user} size={56} />
              <DescriptionList
                className="flex-1"
                termWidth="sm:grid-cols-[120px_minmax(0,1fr)]"
                items={[
                  { term: 'Username', detail: <span className="font-mono">{user.username}</span> },
                  { term: 'Display name', detail: user.display_name },
                  { term: 'Email', detail: user.email || <span className="text-fg-faint">none</span> },
                  {
                    term: 'Role',
                    detail: (
                      <span className="flex items-center gap-2">
                        <Badge tone="accent">{user.role}</Badge>
                        <span className="text-fg-muted">{user.role === 'admin' || user.all_repos ? 'all repositories' : 'selected repositories'}</span>
                      </span>
                    ),
                  },
                ]}
              />
            </div>
          </Card>
          <AppearanceSection />
          <PasswordSection />
          <TwoFactorSection enabled={user.totp_enabled} />
          <TokensSection isAdmin={user.role === 'admin'} />
        </div>
      </TwoColumn>
    </Page>
  )
}

function AppearanceSection() {
  const { pref, setPref } = useTheme()
  return (
    <Card id="appearance" title="Appearance" description="Saved in this browser.">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-medium">Theme</p>
          <p className="text-sm text-fg-muted">System follows your operating system's setting.</p>
        </div>
        <SegmentedControl
          label="Theme"
          value={pref}
          onChange={setPref}
          options={[
            { value: 'system', label: 'System', icon: <Monitor size={12} /> },
            { value: 'light', label: 'Light', icon: <Sun size={12} /> },
            { value: 'dark', label: 'Dark', icon: <Moon size={12} /> },
          ]}
        />
      </div>
    </Card>
  )
}

function PasswordSection() {
  const toast = useToast()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const mismatch = again !== '' && next !== again
  const change = useMutation({
    mutationFn: () => unwrap(api.POST('/account/password', { body: { current_password: current, new_password: next } })),
    onSuccess: () => {
      setCurrent('')
      setNext('')
      setAgain('')
      toast.success('Password changed', { description: 'Your other sessions were signed out.' })
    },
  })
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        if (!mismatch) change.mutate()
      }}
    >
      <Card
        id="password"
        title="Password"
        description="Changing your password signs you out of your other sessions."
        footer={
          <Button type="submit" variant="primary" loading={change.isPending} disabled={!current || next.length < 10 || next !== again}>
            Change password
          </Button>
        }
      >
        <div className="flex max-w-sm flex-col gap-4">
          <Field label="Current password">
            {(id) => <Input id={id} type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />}
          </Field>
          <Field label="New password" hint="At least 10 characters." error={next && next.length < 10 ? `${10 - next.length} more character${next.length === 9 ? '' : 's'} needed.` : undefined}>
            {(id) => <Input id={id} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" minLength={10} required />}
          </Field>
          <Field label="Confirm new password" error={mismatch ? "The passwords don't match." : undefined}>
            {(id) => <Input id={id} type="password" value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" required aria-invalid={mismatch || undefined} />}
          </Field>
          {change.isError && <Alert>{errorMessage(change.error)}</Alert>}
        </div>
      </Card>
    </form>
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
    <Section id="two-factor" title="Two-factor authentication" description="Require a code from an authenticator app when you sign in.">
      <div className="flex flex-wrap items-center gap-3">
        <span className={enabled ? 'flex size-9 items-center justify-center rounded-full bg-success/12 text-success' : 'flex size-9 items-center justify-center rounded-full bg-bg-hover text-fg-faint'}>
          {enabled ? <ShieldCheck size={17} /> : <ShieldOff size={17} />}
        </span>
        <div className="min-w-0 flex-1">
          <p className="font-medium">{enabled ? 'Enabled' : 'Not enabled'}</p>
          <p className="text-sm text-fg-muted">{enabled ? 'Sign-ins need a code from your authenticator app or a recovery code.' : 'Your account is protected by your password only.'}</p>
        </div>
        {enabled ? (
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
        )}
      </div>

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
  const confirm = useConfirm()
  const toast = useToast()
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
    onSuccess: () => {
      toast.success('Token revoked')
      void qc.invalidateQueries({ queryKey: tokensQuery.queryKey })
    },
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
      id="tokens"
      flush
      title="Personal access tokens"
      description={
        <>
          For the <code className="font-mono">tgp</code> CLI, git over HTTPS and scripts. Use them as the password for git, or as{' '}
          <code className="font-mono">Authorization: Bearer …</code>.
        </>
      }
      actions={
        <Button icon={<Plus size={14} />} onClick={() => setOpen(true)}>
          New token
        </Button>
      }
    >
      {tokens.isPending ? (
        <SkeletonRows rows={2} />
      ) : tokens.data?.length === 0 ? (
        <EmptyState size="inline" icon={<KeyRound size={18} />} title="No tokens yet">
          Create one for the CLI or for git over HTTPS.
        </EmptyState>
      ) : (
        <ul>
          {tokens.data?.map((t) => (
            <li key={t.id} className="flex items-center gap-3 border-b border-border px-4 py-3 last:border-0">
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md border border-border bg-bg-subtle text-fg-muted">
                <KeyRound size={14} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{t.name}</span>
                  <code className="font-mono text-sm text-fg-faint">{t.prefix}…</code>
                  {t.scopes.map((s) => (
                    <Badge key={s} tone={s === 'admin' ? 'warning' : 'neutral'}>
                      {s}
                    </Badge>
                  ))}
                </div>
                <div className="text-sm text-fg-faint">
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
              <IconButton
                label={`Revoke ${t.name}`}
                size="sm"
                className="hover:bg-danger/10 hover:text-danger"
                loading={revoke.isPending && revoke.variables === t.id}
                onClick={async () => {
                  const ok = await confirm({ title: `Revoke ${t.name}?`, description: 'Anything using this token stops working immediately.', confirmLabel: 'Revoke token' })
                  if (ok) revoke.mutate(t.id)
                }}
              >
                <Trash2 size={14} />
              </IconButton>
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
              <legend className="mb-1 text-sm font-medium text-fg-muted">Scopes</legend>
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
