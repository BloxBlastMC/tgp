import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { ChevronDown, ChevronRight, CircleCheck, CircleX, Clock, Plus, RotateCcw, Send, Webhook as WebhookIcon } from 'lucide-react'
import { useId, useState, type ReactNode } from 'react'
import { api, errorMessage, unwrap, type Webhook, type WebhookDelivery } from '../../api/client'
import {
  type DeliveryFilters,
  type HookScope,
  webhookDeliveriesQuery,
  webhookDeliveryQuery,
  webhookEventsQuery,
  webhookQuery,
  webhooksQuery,
} from '../../api/queries'
import {
  deliveryLabel,
  deliveryResult,
  deliveryTone,
  eventMode,
  eventsFor,
  eventsSummary,
  type EventMode,
  formatMs,
  prettyBody,
  shortGuid,
  sortedHeaders,
  urlError,
} from '../../lib/webhooks'
import { Alert, Badge, Button, Checkbox, cx, Dialog, Field, Input, Section, Select, Spinner, Time } from '../ui'

// ---- links ----

/** Links to a webhook page in either scope. */
export function HookLink({ scope, id, className, children }: { scope: HookScope; id?: number | 'new'; className?: string; children: ReactNode }) {
  if (scope.kind === 'repo') {
    if (id === undefined)
      return (
        <Link to="/$repo/settings/webhooks" params={{ repo: scope.repo }} className={className}>
          {children}
        </Link>
      )
    if (id === 'new')
      return (
        <Link to="/$repo/settings/webhooks/new" params={{ repo: scope.repo }} className={className}>
          {children}
        </Link>
      )
    return (
      <Link to="/$repo/settings/webhooks/$id" params={{ repo: scope.repo, id: String(id) }} className={className}>
        {children}
      </Link>
    )
  }
  if (id === undefined)
    return (
      <Link to="/admin/webhooks" className={className}>
        {children}
      </Link>
    )
  if (id === 'new')
    return (
      <Link to="/admin/webhooks/new" className={className}>
        {children}
      </Link>
    )
  return (
    <Link to="/admin/webhooks/$id" params={{ id: String(id) }} className={className}>
      {children}
    </Link>
  )
}

function useGoTo(scope: HookScope) {
  const navigate = useNavigate()
  return (id?: number) => {
    if (scope.kind === 'repo') {
      if (id === undefined) void navigate({ to: '/$repo/settings/webhooks', params: { repo: scope.repo } })
      else void navigate({ to: '/$repo/settings/webhooks/$id', params: { repo: scope.repo, id: String(id) } })
    } else if (id === undefined) void navigate({ to: '/admin/webhooks' })
    else void navigate({ to: '/admin/webhooks/$id', params: { id: String(id) } })
  }
}

// ---- status ----

export function DeliveryStatusIcon({ d, size = 14 }: { d: Pick<WebhookDelivery, 'status'>; size?: number }) {
  const tone = deliveryTone(d)
  const label = d.status === 'success' ? 'Delivered' : d.status === 'failed' ? 'Failed' : 'Pending'
  const Icon = tone === 'success' ? CircleCheck : tone === 'danger' ? CircleX : Clock
  return (
    <span title={label} aria-label={label} className={cx('inline-flex shrink-0', tone === 'success' ? 'text-success' : tone === 'danger' ? 'text-danger' : 'text-warning')}>
      <Icon size={size} />
    </span>
  )
}

function LastDelivery({ d }: { d?: WebhookDelivery | null }) {
  if (!d) return <span className="text-sm text-fg-faint">No deliveries yet</span>
  return (
    <span className="flex items-center gap-1.5 text-sm text-fg-muted">
      <DeliveryStatusIcon d={d} size={12} />
      <span className="font-mono">{d.response_status ?? '—'}</span>
      <span className="text-fg-faint">·</span>
      <Time iso={d.delivered_at ?? d.created_at} />
    </span>
  )
}

// ---- list ----

export function WebhookList({ scope }: { scope: HookScope }) {
  const hooks = useQuery(webhooksQuery(scope))
  const description =
    scope.kind === 'org'
      ? 'Organisation webhooks get events from every repository.'
      : 'Webhooks POST a JSON payload to a URL when something happens in this repository.'
  return (
    <Section
      title="Webhooks"
      description={description}
      actions={
        <HookLink scope={scope} id="new" className="inline-flex h-7 items-center gap-1.5 rounded border border-transparent bg-accent px-2.5 text-base font-medium text-accent-fg hover:brightness-110">
          <Plus size={13} /> Add webhook
        </HookLink>
      }
    >
      {hooks.isPending ? (
        <Spinner />
      ) : hooks.isError ? (
        <Alert>{errorMessage(hooks.error)}</Alert>
      ) : hooks.data.length === 0 ? (
        <p className="text-fg-muted">No webhooks yet.</p>
      ) : (
        <ul className="-my-2 divide-y divide-border" aria-label="Webhooks">
          {hooks.data.map((h) => (
            <li key={h.id} className="flex items-center gap-3 py-2">
              <WebhookIcon size={14} className={h.active ? 'text-fg-muted' : 'text-fg-faint'} />
              <div className="min-w-0 flex-1">
                <HookLink scope={scope} id={h.id} className="block truncate font-mono text-base hover:underline">
                  {h.url}
                </HookLink>
                <div className="flex items-center gap-2 text-sm text-fg-faint">
                  <span>{eventsSummary(h.events)}</span>
                  {h.has_secret && <span>· signed</span>}
                  {h.insecure_ssl && <span className="text-warning">· TLS not verified</span>}
                </div>
              </div>
              {!h.active && <Badge>inactive</Badge>}
              <LastDelivery d={h.last_delivery} />
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

// ---- form ----

export function WebhookForm({ scope, hook }: { scope: HookScope; hook?: Webhook }) {
  const qc = useQueryClient()
  const goTo = useGoTo(scope)
  const events = useQuery(webhookEventsQuery)
  const [url, setUrl] = useState(hook?.url ?? '')
  const [secret, setSecret] = useState('')
  const [removeSecret, setRemoveSecret] = useState(false)
  const initialEvents = hook?.events ?? ['push']
  const [mode, setMode] = useState<EventMode>(eventMode(initialEvents))
  const [selected, setSelected] = useState<string[]>(initialEvents.filter((e) => e !== '*'))
  const [active, setActive] = useState(hook?.active ?? true)
  const [insecure, setInsecure] = useState(hook?.insecure_ssl ?? false)
  const [touched, setTouched] = useState(false)
  const uErr = urlError(url)
  const chosen = eventsFor(mode, selected)
  const noEvents = mode === 'custom' && chosen.length === 0

  const save = useMutation({
    mutationFn: async () => {
      if (hook) {
        const body: { url: string; events: string[]; active: boolean; insecure_ssl: boolean; secret?: string } = {
          url: url.trim(),
          events: chosen,
          active,
          insecure_ssl: insecure,
        }
        if (removeSecret) body.secret = ''
        else if (secret) body.secret = secret
        return unwrap(api.PATCH('/webhooks/{id}', { params: { path: { id: hook.id } }, body }))
      }
      const body = { url: url.trim(), events: chosen, active, insecure_ssl: insecure, ...(secret ? { secret } : {}) }
      return scope.kind === 'repo'
        ? unwrap(api.POST('/repos/{repo}/webhooks', { params: { path: { repo: scope.repo } }, body }))
        : unwrap(api.POST('/admin/webhooks', { body }))
    },
    onSuccess: (h) => {
      qc.setQueryData(webhookQuery(h.id).queryKey, h)
      void qc.invalidateQueries({ queryKey: webhooksQuery(scope).queryKey })
      void qc.invalidateQueries({ queryKey: ['webhook', h.id, 'deliveries'] })
      setSecret('')
      setRemoveSecret(false)
      if (!hook) goTo(h.id)
    },
  })

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        setTouched(true)
        if (!uErr && !noEvents) save.mutate()
      }}
    >
      <Field label="Payload URL" hint={touched && uErr ? <span className="text-danger">{uErr}</span> : 'Receives a POST with a JSON body for each event.'}>
        {(id) => <Input id={id} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/hooks/tgp" autoFocus={!hook} />}
      </Field>
      <Field
        label="Secret"
        hint={
          hook?.has_secret && !removeSecret ? (
            <>
              A secret is set; leave empty to keep it.{' '}
              <button type="button" className="text-danger hover:underline" onClick={() => setRemoveSecret(true)}>
                Remove secret
              </button>
            </>
          ) : removeSecret ? (
            <>
              The secret will be removed and deliveries won't be signed.{' '}
              <button type="button" className="text-accent hover:underline" onClick={() => setRemoveSecret(false)}>
                Keep it
              </button>
            </>
          ) : (
            <>
              Signs each delivery with HMAC-SHA256 in <code className="font-mono">X-Tgp-Signature-256</code> (and <code className="font-mono">X-Hub-Signature-256</code>).
            </>
          )
        }
      >
        {(id) => (
          <Input
            id={id}
            type="password"
            autoComplete="new-password"
            value={secret}
            disabled={removeSecret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder={hook?.has_secret ? '••••••••' : ''}
          />
        )}
      </Field>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-sm font-medium text-fg-muted">Which events should trigger this webhook?</legend>
        {(
          [
            ['push', 'Just the push event'],
            ['all', 'Send me everything'],
            ['custom', 'Let me select individual events'],
          ] as const
        ).map(([m, label]) => (
          <label key={m} className="flex items-center gap-2">
            <input type="radio" name="events" checked={mode === m} onChange={() => setMode(m)} className="accent-[var(--accent)]" />
            {label}
          </label>
        ))}
        {mode === 'custom' && (
          <div className="ml-5 grid gap-2 rounded border border-border bg-bg p-3 sm:grid-cols-2" aria-label="Events">
            {events.isPending ? (
              <Spinner />
            ) : (
              events.data?.map((ev) => (
                <EventCheckbox
                  key={ev.name}
                  name={ev.name}
                  description={ev.description}
                  checked={selected.includes(ev.name)}
                  onChange={(v) => setSelected((cur) => (v ? [...cur, ev.name] : cur.filter((x) => x !== ev.name)))}
                />
              ))
            )}
          </div>
        )}
        {touched && noEvents && <p className="text-sm text-danger">Pick at least one event.</p>}
      </fieldset>
      <Checkbox checked={active} onChange={setActive} label="Active" hint="Inactive webhooks keep their settings but get no deliveries." />
      <Checkbox checked={insecure} onChange={setInsecure} label="Skip TLS certificate verification" hint="Only for receivers with self-signed certificates." />
      {insecure && <Alert tone="warning">Without verification anyone on the network path can read or forge deliveries. Prefer a valid certificate.</Alert>}
      {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
      {save.isSuccess && hook && <Alert tone="success">Saved.</Alert>}
      <div className="flex gap-2">
        <Button type="submit" variant="primary" loading={save.isPending}>
          {hook ? 'Update webhook' : 'Add webhook'}
        </Button>
        {!hook && (
          <HookLink scope={scope} className="inline-flex h-7 items-center rounded px-2.5 text-base text-fg-muted hover:bg-bg-hover hover:text-fg">
            Cancel
          </HookLink>
        )}
      </div>
    </form>
  )
}

/** An event checkbox labelled by the event name, described by its description. */
function EventCheckbox({ name, description, checked, onChange }: { name: string; description: string; checked: boolean; onChange: (v: boolean) => void }) {
  const id = useId()
  return (
    <div className="flex items-start gap-2">
      <input
        id={id}
        type="checkbox"
        checked={checked}
        aria-describedby={id + '-d'}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-0.5 size-3.5 accent-[var(--accent)]"
      />
      <div>
        <label htmlFor={id} className="font-mono text-base select-none">
          {name}
        </label>
        <p id={id + '-d'} className="text-sm text-fg-faint">
          {description}
        </p>
      </div>
    </div>
  )
}

// ---- detail ----

export function WebhookDetail({ scope, id }: { scope: HookScope; id: number }) {
  const hook = useQuery(webhookQuery(id))
  const qc = useQueryClient()
  const goTo = useGoTo(scope)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const ping = useMutation({
    mutationFn: () => unwrap(api.POST('/webhooks/{id}/ping', { params: { path: { id } } })),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['webhook', id, 'deliveries'] })
      void qc.invalidateQueries({ queryKey: webhooksQuery(scope).queryKey })
    },
  })
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE('/webhooks/{id}', { params: { path: { id } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: webhooksQuery(scope).queryKey })
      qc.removeQueries({ queryKey: ['webhook', id] })
      goTo()
    },
  })
  if (hook.isPending) return <Spinner />
  if (hook.isError) return <Alert>{errorMessage(hook.error)}</Alert>
  const h = hook.data
  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center gap-2">
        <HookLink scope={scope} className="text-sm text-fg-muted hover:text-fg hover:underline">
          Webhooks
        </HookLink>
        <span className="text-fg-faint">/</span>
        <span className="min-w-0 truncate font-mono text-base">{h.url}</span>
        {!h.active && <Badge>inactive</Badge>}
        <div className="flex-1" />
        <Button onClick={() => ping.mutate()} loading={ping.isPending}>
          <Send size={13} /> Send ping
        </Button>
        <Button variant="danger" onClick={() => setConfirmDelete(true)}>
          Delete
        </Button>
      </div>
      {ping.isError && <Alert>{errorMessage(ping.error)}</Alert>}
      {ping.data && (
        <Alert tone={ping.data.status === 'success' ? 'success' : ping.data.status === 'failed' ? 'danger' : 'warning'}>
          Ping {ping.data.status === 'success' ? 'delivered' : ping.data.status === 'failed' ? 'failed' : 'queued'}: {deliveryResult(ping.data)}
        </Alert>
      )}
      <Section title="Settings">
        <WebhookForm key={h.updated_at} scope={scope} hook={h} />
      </Section>
      <DeliveriesSection hookId={h.id} />
      <Dialog
        open={confirmDelete}
        onClose={() => setConfirmDelete(false)}
        title="Delete webhook?"
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
              Cancel
            </Button>
            <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
              Delete webhook
            </Button>
          </>
        }
      >
        <p>
          Deliveries to <code className="font-mono break-all">{h.url}</code> stop, and its delivery log is deleted.
        </p>
        {remove.isError && <Alert>{errorMessage(remove.error)}</Alert>}
      </Dialog>
    </div>
  )
}

function DeliveriesSection({ hookId }: { hookId: number }) {
  const [filters, setFilters] = useState<DeliveryFilters>({})
  const [open, setOpen] = useState<number | null>(null)
  const list = useQuery(webhookDeliveriesQuery(hookId, filters))
  const page = filters.page ?? 1
  const total = list.data?.total ?? 0
  return (
    <Section
      title="Recent deliveries"
      description="Kept for 30 days. Failed deliveries are retried with backoff."
      actions={
        <Select
          aria-label="Filter by status"
          value={filters.status ?? ''}
          onChange={(e) => setFilters({ status: (e.target.value || undefined) as DeliveryFilters['status'] })}
          className="h-7 w-32"
        >
          <option value="">All</option>
          <option value="success">Delivered</option>
          <option value="failed">Failed</option>
          <option value="pending">Pending</option>
        </Select>
      }
    >
      {list.isPending ? (
        <Spinner />
      ) : list.isError ? (
        <Alert>{errorMessage(list.error)}</Alert>
      ) : list.data.deliveries.length === 0 ? (
        <p className="text-fg-muted">No deliveries{filters.status ? ' with this status' : ''}.</p>
      ) : (
        <ul className="-mx-4 -my-4 divide-y divide-border" aria-label="Deliveries">
          {list.data.deliveries.map((d) => (
            <li key={d.id}>
              <button
                type="button"
                onClick={() => setOpen(open === d.id ? null : d.id)}
                aria-expanded={open === d.id}
                className="flex w-full items-center gap-3 px-4 py-2 text-left hover:bg-bg-hover"
              >
                {open === d.id ? <ChevronDown size={13} className="text-fg-faint" /> : <ChevronRight size={13} className="text-fg-faint" />}
                <DeliveryStatusIcon d={d} />
                <span className="font-mono text-base">{deliveryLabel(d)}</span>
                <span className="font-mono text-sm text-fg-faint" title={d.guid}>
                  {shortGuid(d.guid)}
                </span>
                {d.redelivery && <Badge>redelivery</Badge>}
                <div className="flex-1" />
                <span className={cx('truncate text-sm', d.status === 'failed' ? 'text-danger' : 'text-fg-muted')}>{deliveryResult(d)}</span>
                <span className="w-24 shrink-0 text-right text-sm text-fg-faint">
                  <Time iso={d.created_at} />
                </span>
              </button>
              {open === d.id && <DeliveryDetailView hookId={hookId} deliveryId={d.id} onRedelivered={() => setFilters({})} />}
            </li>
          ))}
        </ul>
      )}
      {total > 30 && (
        <div className="mt-6 flex items-center justify-end gap-2 text-sm text-fg-muted">
          <span>
            Page {page} of {Math.ceil(total / 30)}
          </span>
          <Button variant="ghost" disabled={page <= 1} onClick={() => setFilters({ ...filters, page: page - 1 })}>
            Newer
          </Button>
          <Button variant="ghost" disabled={page * 30 >= total} onClick={() => setFilters({ ...filters, page: page + 1 })}>
            Older
          </Button>
        </div>
      )}
    </Section>
  )
}

function KV({ rows }: { rows: [string, string][] }) {
  if (rows.length === 0) return <p className="text-sm text-fg-faint">None</p>
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 font-mono text-sm">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-fg-muted">{k}:</dt>
          <dd className="break-all">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

function Pre({ children, label }: { children: string; label: string }) {
  return (
    <pre aria-label={label} className="max-h-96 overflow-auto rounded border border-border bg-bg p-3 font-mono text-sm leading-relaxed whitespace-pre-wrap">
      {children}
    </pre>
  )
}

function DeliveryDetailView({ hookId, deliveryId, onRedelivered }: { hookId: number; deliveryId: number; onRedelivered: () => void }) {
  const qc = useQueryClient()
  const d = useQuery(webhookDeliveryQuery(hookId, deliveryId))
  const [tab, setTab] = useState<'request' | 'response'>('request')
  const redeliver = useMutation({
    mutationFn: () => unwrap(api.POST('/webhooks/{id}/deliveries/{delivery}/redeliver', { params: { path: { id: hookId, delivery: deliveryId } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['webhook', hookId, 'deliveries'] })
      onRedelivered()
    },
  })
  if (d.isPending) return <Spinner />
  if (d.isError) return <div className="px-4 pb-3"><Alert>{errorMessage(d.error)}</Alert></div>
  const x = d.data
  return (
    <div className="flex flex-col gap-3 border-t border-border bg-bg-subtle px-4 py-3" aria-label="Delivery detail">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-fg-muted">
        <span>
          Delivery <span className="font-mono text-fg">{x.guid}</span>
        </span>
        <span>
          {x.attempts} attempt{x.attempts === 1 ? '' : 's'}
        </span>
        {x.duration_ms != null && <span>{formatMs(x.duration_ms)}</span>}
        {x.status === 'pending' && x.next_attempt_at && (
          <span>
            next attempt <Time iso={x.next_attempt_at} />
          </span>
        )}
        <div className="flex-1" />
        <Button onClick={() => redeliver.mutate()} loading={redeliver.isPending}>
          <RotateCcw size={13} /> Redeliver
        </Button>
      </div>
      {redeliver.isSuccess && <Alert tone="success">Queued as a new delivery.</Alert>}
      {redeliver.isError && <Alert>{errorMessage(redeliver.error)}</Alert>}
      {x.error && <Alert>{x.error}</Alert>}
      <div className="flex gap-1 border-b border-border" role="tablist">
        {(['request', 'response'] as const).map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={cx('-mb-px border-b-2 px-2 py-1 text-sm capitalize', tab === t ? 'border-accent text-fg' : 'border-transparent text-fg-muted hover:text-fg')}
          >
            {t}
            {t === 'response' && x.response_status != null && <span className="ml-1 font-mono">{x.response_status}</span>}
          </button>
        ))}
      </div>
      {tab === 'request' ? (
        <>
          <p className="font-mono text-sm">
            <span className="text-fg-muted">POST</span> {x.request.url}
          </p>
          <KV rows={sortedHeaders(x.request.headers)} />
          <Pre label="Payload">{JSON.stringify(x.request.payload, null, 2)}</Pre>
        </>
      ) : x.response ? (
        <>
          <KV rows={sortedHeaders(x.response.headers)} />
          {x.response.body ? <Pre label="Response body">{prettyBody(x.response.body)}</Pre> : <p className="text-sm text-fg-faint">Empty body</p>}
          {x.response.truncated && <p className="text-sm text-fg-faint">The response body was truncated.</p>}
        </>
      ) : (
        <p className="text-sm text-fg-faint">No response{x.status === 'pending' ? ' yet' : ''}.</p>
      )}
    </div>
  )
}
