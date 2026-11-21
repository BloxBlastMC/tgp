import { expect, request, test, type APIRequestContext } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { startServer, type TestServer } from './server'
import { guardCSP, watchCSP } from './csp'

guardCSP(test)

let server: TestServer
const shots = process.env.TGP_SHOTS

async function client(): Promise<APIRequestContext> {
  const c = await request.newContext({ baseURL: server.url })
  await c.get('/api/v1/meta')
  return c
}

async function call(c: APIRequestContext, method: 'post' | 'patch', path: string, data: unknown) {
  const csrf = (await c.storageState()).cookies.find((k) => k.name === 'tgp_csrf')?.value ?? ''
  const r = await c[method]('/api/v1' + path, { data, headers: { 'X-CSRF-Token': csrf } })
  if (!r.ok()) throw new Error(`${path}: ${r.status()} ${await r.text()}`)
  return r.status() === 204 ? null : r.json()
}

test.beforeAll(async () => {
  server = await startServer()
  const admin = await client()
  await call(admin, 'post', '/setup', {
    setup_token: server.setupToken,
    admin: { username: 'owen', password: 'owens-password-1', display_name: 'Owen' },
    org: { name: 'BloxBlast' },
    users: [],
  })
  await call(admin, 'post', '/repos', { name: 'game', auto_init: true })
  await admin.dispose()
})

test.afterAll(async () => {
  await server?.stop()
})

test('admin sees server health: status, checks, database, disks and repository usage', async ({ browser }) => {
  const ctx = await browser.newContext({ colorScheme: 'dark', viewport: { width: 1400, height: 900 } })
  watchCSP(ctx)
  const page = await ctx.newPage()
  await page.goto(server.url + '/login')
  await page.getByLabel('Username or email').fill('owen')
  await page.getByLabel('Password').fill('owens-password-1')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('heading', { name: 'Home', exact: true })).toBeVisible()

  await page.goto(server.url + '/admin')
  await page.getByRole('link', { name: 'Health', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/health$/)

  const status = page.getByRole('status', { name: 'Overall status' })
  await expect(status).toBeVisible()
  await expect(status).toHaveAttribute('data-status', /^(ok|warn|fail)$/)
  await expect(page.getByRole('list', { name: 'Health checks' }).getByTestId('health-check').first()).toBeVisible()

  const db = page.getByRole('region', { name: 'Database' })
  await expect(db.getByText('sqlite')).toBeVisible()

  const disks = page.getByRole('region', { name: 'Disks' })
  await expect(disks.getByTestId('disk-row').first()).toBeVisible()

  const repos = page.getByRole('table', { name: 'Repositories by storage' })
  await expect(repos.getByRole('link', { name: 'game' })).toBeVisible()

  await page.getByRole('button', { name: 'Refresh sizes' }).click()
  await expect(repos.getByRole('link', { name: 'game' })).toBeVisible()

  if (shots) {
    mkdirSync(shots, { recursive: true })
    // The app scrolls inside its main pane: show the whole page from the top.
    await page.setViewportSize({ width: 1400, height: 1700 })
    await page.evaluate(() => document.querySelectorAll('*').forEach((e) => { if (e.scrollTop) e.scrollTop = 0 }))
    await page.screenshot({ path: join(shots, 'health.png'), fullPage: true })
  }
  await ctx.close()
})
