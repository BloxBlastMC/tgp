import type { WebhookDelivery } from '../api/client'

/** How a webhook's events are chosen in the form. */
export type EventMode = 'push' | 'all' | 'custom'

export function eventMode(events: string[]): EventMode {
  if (events.includes('*')) return 'all'
  if (events.length === 1 && events[0] === 'push') return 'push'
  return 'custom'
}

/** The events array to send for a form state. */
export function eventsFor(mode: EventMode, selected: string[]): string[] {
  if (mode === 'all') return ['*']
  if (mode === 'push') return ['push']
  return [...new Set(selected)].sort()
}

/** Short text for a webhook's events: "push", "all events", "issues, push +2". */
export function eventsSummary(events: string[], max = 3): string {
  if (events.includes('*')) return 'all events'
  if (events.length === 0) return 'no events'
  const sorted = [...events].sort()
  const shown = sorted.slice(0, max).join(', ')
  return sorted.length > max ? `${shown} +${sorted.length - max}` : shown
}

/** "issues.opened", or just the event name when there's no action. */
export function deliveryLabel(d: Pick<WebhookDelivery, 'event' | 'action'>): string {
  return d.action ? `${d.event}.${d.action}` : d.event
}

export type DeliveryTone = 'success' | 'danger' | 'warning'

export function deliveryTone(d: Pick<WebhookDelivery, 'status'>): DeliveryTone {
  return d.status === 'success' ? 'success' : d.status === 'failed' ? 'danger' : 'warning'
}

/** A one-line result: "200 · 34 ms", "Failed: connection refused", "Pending (attempt 2)". */
export function deliveryResult(d: Pick<WebhookDelivery, 'status' | 'response_status' | 'duration_ms' | 'error' | 'attempts'>): string {
  const parts: string[] = []
  if (d.response_status != null) parts.push(String(d.response_status))
  if (d.duration_ms != null) parts.push(formatMs(d.duration_ms))
  const timing = parts.join(' · ')
  if (d.status === 'success') return timing || 'Delivered'
  if (d.status === 'pending') return d.attempts > 0 ? `Retrying (${d.attempts} attempt${d.attempts === 1 ? '' : 's'} so far)` : 'Pending'
  const why = d.error || (d.response_status != null ? `HTTP ${d.response_status}` : 'failed')
  return timing && d.response_status == null ? `${why} · ${timing}` : why
}

export function formatMs(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} s`
}

export function shortGuid(guid: string): string {
  return guid.length > 8 ? guid.slice(0, 8) : guid
}

/** Pretty-prints a response body when it's JSON; returns it unchanged otherwise. */
export function prettyBody(body: string): string {
  const t = body.trim()
  if (!(t.startsWith('{') || t.startsWith('['))) return body
  try {
    return JSON.stringify(JSON.parse(t), null, 2)
  } catch {
    return body
  }
}

/** Client-side URL check matching what the server accepts. */
export function urlError(url: string): string | null {
  const u = url.trim()
  if (!u) return 'Enter the URL that should receive events.'
  let parsed: URL
  try {
    parsed = new URL(u)
  } catch {
    return "That isn't a valid URL."
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return 'Use an http:// or https:// URL.'
  return null
}

export function sortedHeaders(h: Record<string, string>): [string, string][] {
  return Object.entries(h).sort(([a], [b]) => a.localeCompare(b))
}
