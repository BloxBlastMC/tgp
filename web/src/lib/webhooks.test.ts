import { describe, expect, it } from 'vitest'
import { deliveryLabel, deliveryResult, deliveryTone, eventMode, eventsFor, eventsSummary, formatMs, lastDeliveryText, prettyBody, shortGuid, urlError } from './webhooks'

describe('webhook helpers', () => {
  it('maps events to form modes and back', () => {
    expect(eventMode(['push'])).toBe('push')
    expect(eventMode(['*'])).toBe('all')
    expect(eventMode(['issues', 'push'])).toBe('custom')
    expect(eventMode([])).toBe('custom')
    expect(eventsFor('all', ['issues'])).toEqual(['*'])
    expect(eventsFor('push', ['issues'])).toEqual(['push'])
    expect(eventsFor('custom', ['push', 'issues', 'push'])).toEqual(['issues', 'push'])
  })
  it('summarises events', () => {
    expect(eventsSummary(['*'])).toBe('all events')
    expect(eventsSummary(['push'])).toBe('push')
    expect(eventsSummary(['release', 'issues', 'push', 'pull_request'])).toBe('issues, pull_request, push +1')
  })
  it('labels and tones deliveries', () => {
    expect(deliveryLabel({ event: 'issues', action: 'opened' })).toBe('issues.opened')
    expect(deliveryLabel({ event: 'push', action: null })).toBe('push')
    expect(deliveryTone({ status: 'success' })).toBe('success')
    expect(deliveryTone({ status: 'failed' })).toBe('danger')
    expect(deliveryTone({ status: 'pending' })).toBe('warning')
  })
  it('describes results', () => {
    expect(deliveryResult({ status: 'success', response_status: 200, duration_ms: 34, attempts: 1 })).toBe('200 · 34 ms')
    expect(deliveryResult({ status: 'failed', response_status: 500, duration_ms: 1200, attempts: 6 })).toBe('HTTP 500')
    expect(deliveryResult({ status: 'failed', error: 'connection refused', attempts: 6 })).toBe('connection refused')
    expect(deliveryResult({ status: 'pending', attempts: 0 })).toBe('Pending')
    expect(deliveryResult({ status: 'pending', attempts: 2 })).toBe('Retrying (2 attempts so far)')
  })
  it('formats', () => {
    expect(formatMs(12.4)).toBe('12 ms')
    expect(formatMs(1500)).toBe('1.5 s')
    expect(formatMs(12_000)).toBe('12 s')
    expect(shortGuid('0f8e2d1c-aaaa')).toBe('0f8e2d1c')
    expect(prettyBody('{"ok":true}')).toBe('{\n  "ok": true\n}')
    expect(prettyBody('plain')).toBe('plain')
    expect(prettyBody('{broken')).toBe('{broken')
  })
  it('checks urls', () => {
    expect(urlError('')).toMatch(/Enter/)
    expect(urlError('ftp://x')).toMatch(/http/)
    expect(urlError('nope')).toMatch(/valid/)
    expect(urlError('https://hooks.example.com/x')).toBeNull()
  })
})

describe('lastDeliveryText', () => {
  it('describes the latest delivery in words', () => {
    expect(lastDeliveryText(null)).toBe('Never delivered')
    expect(lastDeliveryText({ status: 'success', response_status: 200, error: null })).toBe('Delivered (200)')
    expect(lastDeliveryText({ status: 'failed', response_status: 500, error: null })).toBe('Last delivery failed (500)')
    expect(lastDeliveryText({ status: 'failed', response_status: null, error: 'connection refused' })).toBe('Last delivery failed: connection refused')
    expect(lastDeliveryText({ status: 'pending', response_status: null, error: null })).toBe('Delivery pending')
  })
})
