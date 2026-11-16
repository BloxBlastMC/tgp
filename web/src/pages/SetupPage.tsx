import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ArrowRight, Check, Pencil, Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api, ApiRequestError, ensureCsrf, errorMessage, unwrap, type Schemas } from '../api/client'
import { AuthCard } from '../components/AuthCard'
import { Alert, Avatar, Badge, Button, Checkbox, cx, Field, FileDrop, IconButton, Input, SecretValue, Select } from '../components/ui'
import { errorText, uploadLogo } from '../lib/upload'

type Role = Schemas['Role']
type NewUser = { username: string; display_name: string; email: string; role: Role; all_repos: boolean }

const steps = ['Setup token', 'Admin account', 'Organization', 'Team', 'Review'] as const

export function SetupPage() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [step, setStep] = useState(0)
  const [token, setToken] = useState('')
  const [admin, setAdmin] = useState({ username: '', display_name: '', email: '', password: '', confirm: '' })
  const [org, setOrg] = useState({ name: '', description: '', base_url: window.location.origin })
  const [logo, setLogo] = useState<File | null>(null)
  const [users, setUsers] = useState<NewUser[]>([])
  const [logoError, setLogoError] = useState('')
  // Field errors show once you've tried to continue (or as soon as a confirmation mismatches).
  const [tried, setTried] = useState(-1)
  const logoUrl = useMemo(() => (logo ? URL.createObjectURL(logo) : ''), [logo])
  useEffect(() => () => URL.revokeObjectURL(logoUrl), [logoUrl])

  const submit = useMutation({
    mutationFn: async () => {
      await ensureCsrf()
      const res = await unwrap(
        api.POST('/setup', {
          body: {
            setup_token: token.trim(),
            admin: { username: admin.username, display_name: admin.display_name, email: admin.email, password: admin.password },
            org,
            users: users.filter((u) => u.username.trim() !== ''),
          },
        }),
      )
      // Setup logged us in as the admin, so the logo can be uploaded now.
      if (logo) {
        try {
          await uploadLogo(logo)
        } catch (e) {
          setLogoError(e instanceof Error ? e.message : errorText('', 'Logo upload failed'))
        }
      }
      return res
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['meta'] })
      void qc.invalidateQueries({ queryKey: ['session'] })
    },
    onError: (e) => {
      if (e instanceof ApiRequestError && e.status === 403) setStep(0)
    },
  })

  if (submit.data) {
    return (
      <AuthCard title="You're all set" subtitle={`${org.name} is ready.`} wide>
        <div className="flex flex-col gap-4">
          {logoError && <Alert tone="warning">The logo wasn't saved: {logoError}. You can upload it later under Admin → Organization.</Alert>}
          {submit.data.users.length > 0 && (
            <>
              <Alert tone="warning">
                Copy these temporary passwords now: they won't be shown again. Each person sets their own password at first login. They expire in 72 hours.
              </Alert>
              <div className="flex flex-col gap-3">
                {submit.data.users.map((u) => (
                  <div key={u.user.id}>
                    <div className="mb-1.5 flex items-center gap-2">
                      <Avatar user={u.user} size={20} />
                      <span className="font-medium">{u.user.username}</span>
                      <Badge>{u.user.role}</Badge>
                    </div>
                    <SecretValue value={u.temp_password} label={`Temporary password for ${u.user.username}`} />
                  </div>
                ))}
              </div>
            </>
          )}
          <Button variant="primary" size="lg" iconRight={<ArrowRight size={15} />} onClick={() => void navigate({ to: '/' })}>
            Continue to {org.name}
          </Button>
        </div>
      </AuthCard>
    )
  }

  const adminErrors = {
    username: !admin.username.trim() ? 'Choose a username.' : null,
    password: admin.password.length < 10 ? 'Use at least 10 characters.' : null,
    confirm: admin.password !== admin.confirm ? "The passwords don't match." : null,
  }
  const adminValid = !adminErrors.username && !adminErrors.password && !adminErrors.confirm
  const canNext = [token.trim() !== '', adminValid, org.name.trim() !== '', true, true][step]
  const show = tried === step
  const go = (to: number) => {
    setTried(-1)
    setStep(to)
  }

  return (
    <AuthCard title="Set up tgp" subtitle="Create the first admin and configure your team's server." wide>
      <ol className="mb-6 flex items-center" aria-label="Setup steps">
        {steps.map((s, i) => {
          const done = i < step
          const current = i === step
          return (
            <li key={s} className={cx('flex items-center', i < steps.length - 1 && 'flex-1')} aria-current={current ? 'step' : undefined}>
              <button
                type="button"
                disabled={!done}
                onClick={() => go(i)}
                title={s}
                className={cx('flex flex-col items-center gap-1.5 text-xs font-medium sm:flex-row sm:gap-2', current ? 'text-fg' : done ? 'cursor-pointer text-fg-muted hover:text-fg' : 'text-fg-faint')}
              >
                <span
                  className={cx(
                    'flex size-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold transition-colors',
                    done ? 'border-transparent bg-success text-white' : current ? 'border-transparent bg-accent-solid text-accent-fg' : 'border-border-strong bg-surface-1',
                  )}
                >
                  {done ? <Check size={12} /> : i + 1}
                </span>
                {current && <span className="hidden whitespace-nowrap md:inline">{s}</span>}
              </button>
              {i < steps.length - 1 && <span className={cx('mx-2 h-px min-w-4 flex-1', done ? 'bg-success/60' : 'bg-border-strong')} aria-hidden />}
            </li>
          )
        })}
      </ol>
      <p className="mb-4 text-xs font-semibold tracking-wide text-fg-faint uppercase md:hidden">
        Step {step + 1} of {steps.length}: {steps[step]}
      </p>

      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          if (!canNext) {
            setTried(step)
            return
          }
          if (step < steps.length - 1) go(step + 1)
          else submit.mutate()
        }}
      >
        {step === 0 && (
          <>
            <p className="text-fg-muted">
              When tgp-server starts for the first time it prints a one-time <strong className="text-fg">setup token</strong> in its log. This
              proves you run the server.
            </p>
            <Field label="Setup token" hint="Restarting the server prints a new token." error={show && !token.trim() ? 'Paste the setup token from the server log.' : undefined}>
              {(id) => <Input id={id} value={token} onChange={(e) => setToken(e.target.value)} className="font-mono" autoFocus autoComplete="off" />}
            </Field>
          </>
        )}

        {step === 1 && (
          <>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Username" error={show ? adminErrors.username : undefined}>
                {(id) => <Input id={id} value={admin.username} onChange={(e) => setAdmin({ ...admin, username: e.target.value.toLowerCase() })} autoComplete="username" autoFocus />}
              </Field>
              <Field label="Display name">
                {(id) => <Input id={id} value={admin.display_name} onChange={(e) => setAdmin({ ...admin, display_name: e.target.value })} autoComplete="name" />}
              </Field>
            </div>
            <Field label="Email" hint="Optional. Can also be used to log in.">
              {(id) => <Input id={id} type="email" value={admin.email} onChange={(e) => setAdmin({ ...admin, email: e.target.value })} autoComplete="email" />}
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Password" hint="At least 10 characters." error={show ? adminErrors.password : undefined}>
                {(id) => <Input id={id} type="password" value={admin.password} onChange={(e) => setAdmin({ ...admin, password: e.target.value })} autoComplete="new-password" />}
              </Field>
              <Field label="Confirm password" error={(show || admin.confirm !== '') && admin.confirm !== admin.password ? adminErrors.confirm : undefined}>
                {(id) => <Input id={id} type="password" value={admin.confirm} onChange={(e) => setAdmin({ ...admin, confirm: e.target.value })} autoComplete="new-password" />}
              </Field>
            </div>
          </>
        )}

        {step === 2 && (
          <>
            <Field label="Organization name" error={show && !org.name.trim() ? 'Name your organization.' : undefined}>
              {(id) => <Input id={id} value={org.name} onChange={(e) => setOrg({ ...org, name: e.target.value })} placeholder="BloxBlast" autoFocus />}
            </Field>
            <Field label="Description" hint="Optional.">
              {(id) => <Input id={id} value={org.description} onChange={(e) => setOrg({ ...org, description: e.target.value })} />}
            </Field>
            <Field label="Base URL" hint="The public address people use to reach this server. Used in clone URLs and webhooks.">
              {(id) => <Input id={id} value={org.base_url} onChange={(e) => setOrg({ ...org, base_url: e.target.value })} placeholder="https://git.example.com" />}
            </Field>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm font-medium">Logo</span>
              <div className="flex items-center gap-3">
                {logoUrl && <img src={logoUrl} alt="" className="size-10 rounded-lg border border-border object-cover" />}
                <FileDrop compact className="flex-1" label="Logo" accept="image/png,image/jpeg,image/gif,image/webp" onFiles={(f) => setLogo(f[0] ?? null)} hint="Optional. PNG, JPEG, GIF or WebP, up to 512 KiB.">
                  <span className="font-medium text-fg">{logo ? logo.name : 'Drop an image or browse'}</span>
                </FileDrop>
                {logo && (
                  <IconButton label="Remove logo" onClick={() => setLogo(null)}>
                    <Trash2 size={14} />
                  </IconButton>
                )}
              </div>
            </div>
          </>
        )}

        {step === 3 && (
          <>
            <p className="text-fg-muted">
              Optionally add your team now. Each person gets a temporary password to log in with and then chooses their own. You can add people
              later from the admin panel.
            </p>
            <div className="flex flex-col gap-2">
              {users.map((u, i) => (
                <div key={i} className="flex flex-col gap-2.5 rounded-lg border border-border bg-bg-subtle p-3">
                  <div className="grid grid-cols-[1fr_1fr_auto] gap-2">
                    <Input
                      aria-label="Username"
                      placeholder="username"
                      value={u.username}
                      onChange={(e) => setUsers(users.map((x, j) => (j === i ? { ...x, username: e.target.value.toLowerCase() } : x)))}
                    />
                    <Select
                      aria-label="Role"
                      value={u.role}
                      onChange={(e) => setUsers(users.map((x, j) => (j === i ? { ...x, role: e.target.value as Role } : x)))}
                    >
                      <option value="maintainer">Maintainer</option>
                      <option value="tester">Tester</option>
                      <option value="admin">Admin</option>
                    </Select>
                    <IconButton label="Remove" onClick={() => setUsers(users.filter((_, j) => j !== i))} className="hover:bg-danger/10 hover:text-danger">
                      <Trash2 size={14} />
                    </IconButton>
                  </div>
                  <div className="grid grid-cols-2 gap-2">
                    <Input
                      aria-label="Display name"
                      placeholder="Display name (optional)"
                      value={u.display_name}
                      onChange={(e) => setUsers(users.map((x, j) => (j === i ? { ...x, display_name: e.target.value } : x)))}
                    />
                    <Input
                      aria-label="Email"
                      placeholder="Email (optional)"
                      value={u.email}
                      onChange={(e) => setUsers(users.map((x, j) => (j === i ? { ...x, email: e.target.value } : x)))}
                    />
                  </div>
                  {u.role !== 'admin' && (
                    <Checkbox
                      checked={u.all_repos}
                      onChange={(v) => setUsers(users.map((x, j) => (j === i ? { ...x, all_repos: v } : x)))}
                      label="Access to all repositories"
                      hint="Otherwise grant specific repositories after creating them."
                    />
                  )}
                </div>
              ))}
              <Button
                className="self-start"
                icon={<Plus size={14} />}
                onClick={() => setUsers([...users, { username: '', display_name: '', email: '', role: 'maintainer', all_repos: true }])}
              >
                Add person
              </Button>
            </div>
          </>
        )}

        {step === 4 && (
          <div className="overflow-hidden rounded-lg border border-border">
            {(
              [
                [
                  1,
                  'Admin',
                  <>
                    {admin.username}
                    {admin.email && <span className="text-fg-muted"> · {admin.email}</span>}
                  </>,
                ],
                [2, 'Organization', org.name],
                [2, 'Base URL', org.base_url || <span className="text-fg-faint">not set</span>],
                [2, 'Logo', logo ? logo.name : <span className="text-fg-faint">none</span>],
                [
                  3,
                  'Team members',
                  users.filter((u) => u.username).length === 0 ? (
                    <span className="text-fg-faint">none yet</span>
                  ) : (
                    <div className="flex flex-wrap gap-1.5">
                      {users
                        .filter((u) => u.username)
                        .map((u) => (
                          <span key={u.username} className="inline-flex items-center gap-1">
                            {u.username} <Badge>{u.role}</Badge>
                          </span>
                        ))}
                    </div>
                  ),
                ],
              ] as const
            ).map(([to, term, detail]) => (
              <div key={term} className="flex items-start gap-3 border-b border-border px-4 py-2.5 last:border-0">
                <span className="w-28 shrink-0 text-fg-muted">{term}</span>
                <span className="min-w-0 flex-1 break-words">{detail}</span>
                <IconButton label={`Edit ${term.toLowerCase()}`} size="xs" onClick={() => go(to)}>
                  <Pencil size={12} />
                </IconButton>
              </div>
            ))}
          </div>
        )}

        {submit.isError && <Alert>{errorMessage(submit.error)}</Alert>}

        <div className="mt-2 flex justify-between border-t border-border pt-4">
          {step > 0 ? (
            <Button variant="ghost" onClick={() => go(step - 1)}>
              Back
            </Button>
          ) : (
            <span />
          )}
          <Button type="submit" variant="primary" size="lg" loading={submit.isPending}>
            {step === steps.length - 1 ? 'Complete setup' : step === 3 && users.length === 0 ? 'Skip' : 'Continue'}
          </Button>
        </div>
      </form>
    </AuthCard>
  )
}
