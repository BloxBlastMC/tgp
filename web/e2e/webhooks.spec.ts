import { expect, request, test, type APIRequestContext, type Browser, type Page } from '@playwright/test'
import { createHmac } from 'node:crypto'
import { mkdirSync } from 'node:fs'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { join } from 'node:path'
import { startServer, type TestServer } from './server'

let server: TestServer
const shots = process.env.TGP_SHOTS

type Received = { event: string; delivery: string; signature: string; body: string }
let receiver: Server
let receiverUrl = ''
const received: Received[] = []

async function client(): Promise<APIRequestContext> {
  const c = await request.newContext({ baseURL: server.url })
  await c.get('/api/v1/meta')
  return c
}

async function call(c: APIRequestContext, method: 'post' | 'put' | 'patch', path: string, data: unknown) {
  const csrf = (await c.storageState()).cookies.find((k) => k.name === 'tgp_csrf')?.value ?? ''
  const r = await c[method]('/api/v1' + path, { data, headers: { 'X-CSRF-Token': csrf } })
  if (!r.ok()) throw new Error(`${path}: ${r.status()} ${await r.text()}`)
  return r.status() === 204 ? null : r.json()
}

async function loginAs(browser: Browser, username: string, password: string): Promise<Page> {
  const ctx = await browser.newContext({ colorScheme: 'dark', viewport: { width: 1400, height: 900 } })
  const page = await ctx.newPage()
  await page.goto(server.url + '/login')
  await page.getByLabel('Username or email').fill(username)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('heading', { name: 'Repositories' })).toBeVisible()
  return page
}

async function shot(page: Page, name: string) {
  if (!shots) return
  mkdirSync(shots, { recursive: true })
  await page.screenshot({ path: join(shots, name), fullPage: false })
}

test.beforeAll(async () => {
  // A receiver that records deliveries and answers 200 with a JSON body.
  receiver = createServer((req, res) => {
    let body = ''
    req.on('data', (c: Buffer) => (body += c.toString()))
    req.on('end', () => {
      received.push({
        event: String(req.headers['x-tgp-event'] ?? ''),
        delivery: String(req.headers['x-tgp-delivery'] ?? ''),
        signature: String(req.headers['x-tgp-signature-256'] ?? ''),
        body,
      })
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end('{"ok":true}')
    })
  })
  await new Promise<void>((res) => receiver.listen(0, '127.0.0.1', res))
  receiverUrl = `http://127.0.0.1:${(receiver.address() as AddressInfo).port}/hook`

  server = await startServer()
  const admin = await client()
  const setup = await call(admin, 'post', '/setup', {
    setup_token: server.setupToken,
    admin: { username: 'owen', password: 'owens-password-1', display_name: 'Owen' },
    org: { name: 'BloxBlast' },
    users: [
      { username: 'mia', display_name: 'Mia', role: 'maintainer', all_repos: true },
      { username: 'tom', display_name: 'Tom', role: 'tester', all_repos: true },
    ],
  })
  for (const u of setup.users as { user: { username: string }; temp_password: string }[]) {
    const c = await client()
    await call(c, 'post', '/auth/login', { username: u.user.username, password: u.temp_password })
    await call(c, 'post', '/account/password', { current_password: u.temp_password, new_password: `${u.user.username}s-password-1` })
    await c.dispose()
  }
  await call(admin, 'post', '/repos', { name: 'game', auto_init: true })
  await admin.dispose()
})

test.afterAll(async () => {
  await server?.stop()
  await new Promise((res) => receiver?.close(res))
})

test('maintainer adds a signed webhook, sees deliveries and redelivers; testers have no webhook settings', async ({ browser }) => {
  const mia = await loginAs(browser, 'mia', 'mias-password-1')
  await mia.goto(server.url + '/game/settings')
  await mia.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Webhooks' }).click()
  await expect(mia.getByText('No webhooks yet.')).toBeVisible()
  await mia.getByRole('link', { name: 'Add webhook' }).click()

  await mia.getByLabel('Payload URL').fill(receiverUrl)
  await mia.getByLabel('Secret').fill('hook-secret-1')
  await mia.getByLabel('Let me select individual events').check()
  await mia.getByLabel('issues', { exact: true }).check()
  await mia.getByLabel('push', { exact: true }).check()
  await shot(mia, 'webhook-form.png')
  await mia.getByRole('button', { name: 'Add webhook' }).click()

  // The detail page shows the ping delivery.
  const deliveries = mia.getByRole('list', { name: 'Deliveries' })
  await expect(deliveries.getByText('ping')).toBeVisible({ timeout: 15_000 })
  await expect(deliveries.getByRole('button').first()).toContainText('200')
  await expect.poll(() => received.find((r) => r.event === 'ping')).toBeTruthy()
  const ping = received.find((r) => r.event === 'ping')!
  expect(ping.signature).toBe('sha256=' + createHmac('sha256', 'hook-secret-1').update(ping.body).digest('hex'))

  // An issue opened through the API produces an issues.opened delivery.
  const api = await client()
  await call(api, 'post', '/auth/login', { username: 'mia', password: 'mias-password-1' })
  await call(api, 'post', '/repos/game/issues', { title: 'Lag spikes in lobby' })
  await api.dispose()
  await expect.poll(() => received.filter((r) => r.event === 'issues').length, { timeout: 15_000 }).toBe(1)
  await mia.reload()
  const row = deliveries.getByRole('button', { name: /issues\.opened/ })
  await expect(row).toBeVisible()
  await row.click()
  const detail = mia.getByLabel('Delivery detail')
  await expect(detail.getByLabel('Payload', { exact: true })).toContainText('Lag spikes in lobby')
  await expect(detail.getByText(/^x-tgp-event:$/i)).toBeVisible()
  await detail.getByRole('tab', { name: /response/i }).click()
  await expect(detail.getByLabel('Response body')).toContainText('"ok": true')
  await shot(mia, 'webhook-deliveries.png')

  // Redeliver: a second issues delivery arrives with a new GUID.
  await detail.getByRole('button', { name: 'Redeliver' }).click()
  await expect(detail.getByText('Queued as a new delivery.')).toBeVisible()
  await expect.poll(() => received.filter((r) => r.event === 'issues').length, { timeout: 15_000 }).toBe(2)
  const [first, second] = received.filter((r) => r.event === 'issues')
  expect(second!.delivery).not.toBe(first!.delivery)
  await expect(deliveries.getByText('redelivery')).toBeVisible({ timeout: 10_000 })

  // The list shows the hook with its last delivery.
  await mia.getByRole('link', { name: 'Webhooks', exact: true }).first().click()
  await expect(mia.getByRole('list', { name: 'Webhooks' }).getByText(receiverUrl)).toBeVisible()
  await expect(mia.getByText('issues, push')).toBeVisible()
  await shot(mia, 'webhooks.png')

  // Testers don't get settings or webhook pages.
  const tom = await loginAs(browser, 'tom', 'toms-password-1')
  await tom.goto(server.url + '/game')
  await expect(tom.getByRole('link', { name: 'Issues' }).first()).toBeVisible()
  await expect(tom.locator('a[href="/game/settings"]')).toHaveCount(0)
  await tom.goto(server.url + '/game/settings/webhooks')
  await expect(tom.getByText('You need write access to manage webhooks.')).toBeVisible()
})
