import { expect, request, test, type APIRequestContext, type Browser, type Page } from '@playwright/test'
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
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

async function call(c: APIRequestContext, method: 'post' | 'put' | 'patch', path: string, data: unknown) {
  const csrf = (await c.storageState()).cookies.find((k) => k.name === 'tgp_csrf')?.value ?? ''
  const r = await c[method]('/api/v1' + path, { data, headers: { 'X-CSRF-Token': csrf } })
  if (!r.ok()) throw new Error(`${path}: ${r.status()} ${await r.text()}`)
  return r.status() === 204 ? null : r.json()
}

async function loginAs(browser: Browser, username: string, password: string): Promise<Page> {
  const ctx = await browser.newContext({ colorScheme: 'dark', viewport: { width: 1400, height: 900 }, acceptDownloads: true })
  watchCSP(ctx)
  const page = await ctx.newPage()
  await page.goto(server.url + '/login')
  await page.getByLabel('Username or email').fill(username)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('heading', { name: 'Home', exact: true })).toBeVisible()
  return page
}

async function shot(page: Page, name: string) {
  if (!shots) return
  mkdirSync(shots, { recursive: true })
  await page.screenshot({ path: join(shots, name), fullPage: false })
}

test.beforeAll(async () => {
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
})

test('maintainer publishes a release with a file; a tester can download but not edit', async ({ browser }) => {
  const mia = await loginAs(browser, 'mia', 'mias-password-1')
  await mia.goto(server.url + '/game/releases')
  await expect(mia.getByText('No releases yet')).toBeVisible()
  await mia.getByRole('link', { name: 'New release' }).click()

  const form = mia.getByRole('form', { name: 'New release' })
  await form.getByLabel('Tag').fill('v1.0.0')
  await expect(form.getByText('New tag — created from the target')).toBeVisible()
  await expect(form.getByLabel('Target')).toHaveValue('main')
  await form.getByLabel('Title').fill('First playable')
  await form.getByLabel('Release notes').fill('## Highlights\n\n- Minigames lobby')

  const dir = mkdtempSync(join(tmpdir(), 'tgp-release-'))
  const file = join(dir, 'game-linux-amd64.tar.gz')
  writeFileSync(file, 'pretend this is a build\n')
  await mia.getByLabel('Attach files').setInputFiles(file)
  await expect(form.getByText('game-linux-amd64.tar.gz')).toBeVisible()
  await shot(mia, 'release-new.png')
  await form.getByRole('button', { name: 'Publish release' }).click()

  // The release page, reached by tag.
  await expect(mia).toHaveURL(/\/game\/releases\/tag\/v1\.0\.0$/)
  const card = mia.getByRole('article', { name: 'Release First playable' })
  await expect(card.getByText('Latest')).toBeVisible()
  await expect(card.getByRole('heading', { name: 'Highlights' })).toBeVisible()
  await expect(card.getByRole('link', { name: 'game-linux-amd64.tar.gz' })).toBeVisible()
  await expect(card.getByRole('link', { name: 'Source code (zip)' })).toBeVisible()

  // The list shows it too, and the tag now exists.
  await mia.getByRole('link', { name: 'Releases' }).first().click()
  await expect(mia.getByRole('article', { name: 'Release First playable' }).getByText('Latest')).toBeVisible()
  await shot(mia, 'releases.png')

  // A tester sees the release and downloads the file, without edit controls.
  const tom = await loginAs(browser, 'tom', 'toms-password-1')
  await tom.goto(server.url + '/game/releases')
  const tcard = tom.getByRole('article', { name: 'Release First playable' })
  await expect(tcard).toBeVisible()
  await expect(tom.getByRole('link', { name: 'New release' })).toHaveCount(0)
  await expect(tcard.getByRole('button', { name: 'Edit release' })).toHaveCount(0)
  await expect(tcard.getByRole('button', { name: 'Delete release' })).toHaveCount(0)
  const [download] = await Promise.all([tom.waitForEvent('download'), tcard.getByRole('link', { name: 'game-linux-amd64.tar.gz' }).click()])
  expect(readFileSync(await download.path()).toString()).toBe('pretend this is a build\n')
  await tom.reload()
  await expect(tom.getByRole('article', { name: 'Release First playable' }).getByTitle('Downloads')).toHaveText('1')

  // A draft stays hidden from the tester.
  const c = await client()
  await call(c, 'post', '/auth/login', { username: 'mia', password: 'mias-password-1' })
  await call(c, 'post', '/repos/game/releases', { tag: 'v1.1.0', name: 'Next', draft: true })
  await c.dispose()
  await mia.reload()
  await expect(mia.getByRole('article', { name: 'Release Next' }).getByText('Draft')).toBeVisible()
  await tom.reload()
  await expect(tom.getByRole('article', { name: 'Release First playable' })).toBeVisible()
  await expect(tom.getByRole('article', { name: 'Release Next' })).toHaveCount(0)

  // The maintainer deletes the draft.
  const draft = mia.getByRole('article', { name: 'Release Next' })
  await draft.getByRole('button', { name: 'Delete release' }).click()
  await mia.getByRole('dialog', { name: 'Delete Next?' }).getByRole('button', { name: 'Delete release' }).click()
  await expect(mia.getByRole('article', { name: 'Release Next' })).toHaveCount(0)
})
