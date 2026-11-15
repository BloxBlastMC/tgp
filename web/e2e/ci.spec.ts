import { expect, request, test, type APIRequestContext, type Browser, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { startServer, type TestServer } from './server'

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

function git(cwd: string, ...args: string[]) {
  return execFileSync('git', args, {
    cwd,
    env: { ...process.env, HOME: cwd, GIT_CONFIG_NOSYSTEM: '1', GIT_TERMINAL_PROMPT: '0', GIT_AUTHOR_NAME: 'Mia', GIT_AUTHOR_EMAIL: 'mia@example.com', GIT_COMMITTER_NAME: 'Mia', GIT_COMMITTER_EMAIL: 'mia@example.com' },
    stdio: ['ignore', 'pipe', 'pipe'],
  }).toString()
}

async function loginAs(browser: Browser, username: string, password: string): Promise<Page> {
  const ctx = await browser.newContext({ colorScheme: 'dark', viewport: { width: 1400, height: 900 } })
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
    users: [{ username: 'mia', display_name: 'Mia', role: 'maintainer', all_repos: true }],
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

test('admin registers a runner token and changes CI settings', async ({ browser }) => {
  const owen = await loginAs(browser, 'owen', 'owens-password-1')
  await owen.goto(server.url + '/admin/runners')
  await expect(owen.getByText('No runners registered')).toBeVisible()
  await owen.getByRole('button', { name: 'Register runner' }).first().click()
  const dlg = owen.getByRole('dialog', { name: 'Register a runner' })
  await expect(dlg.getByLabel('Registration token')).not.toBeEmpty()
  await expect(dlg.getByLabel('Register command')).toContainText('tgp-runner register')
  await shot(owen, 'ci-runner-token.png')
  await dlg.getByRole('button', { name: 'Done' }).click()
  await expect(owen.getByText('No runners registered')).toBeVisible()

  await owen.getByRole('link', { name: 'CI settings' }).click()
  const perRepo = owen.getByLabel('Max jobs per repository')
  await perRepo.fill('3')
  await owen.getByLabel('Default image').fill('debian:trixie')
  await owen.getByRole('button', { name: 'Save CI settings' }).click()
  await expect(owen.getByText('Saved.')).toBeVisible()
  await owen.reload()
  await expect(owen.getByLabel('Max jobs per repository')).toHaveValue('3')
  await expect(owen.getByLabel('Default image')).toHaveValue('debian:trixie')

  // Organization secret.
  await owen.getByRole('button', { name: 'Add secret' }).click()
  const sd = owen.getByRole('dialog', { name: 'Add secret' })
  await sd.getByLabel('Name').fill('registry_password')
  await sd.getByLabel('Value').fill('hunter2')
  await sd.getByRole('button', { name: 'Add secret' }).click()
  await expect(owen.getByRole('list', { name: 'Organization secrets' }).getByText('REGISTRY_PASSWORD')).toBeVisible()
  await owen.context().close()
})

test('maintainer adds a repository secret', async ({ browser }) => {
  const mia = await loginAs(browser, 'mia', 'mias-password-1')
  await mia.goto(server.url + '/game/settings/secrets')
  await mia.getByRole('button', { name: 'Add secret' }).click()
  const sd = mia.getByRole('dialog', { name: 'Add secret' })
  await sd.getByLabel('Name').fill('deploy-token')
  await expect(sd.getByLabel('Name')).toHaveValue('DEPLOY_TOKEN')
  await sd.getByLabel('Value').fill('s3cret value')
  await sd.getByRole('button', { name: 'Add secret' }).click()
  const list = mia.getByRole('list', { name: 'Repository secrets' })
  await expect(list.getByText('DEPLOY_TOKEN')).toBeVisible()
  await expect(mia.getByText('s3cret value')).toHaveCount(0)
  await mia.context().close()
})

test('a pushed workflow queues a run that can be cancelled; invalid workflows show errors', async ({ browser }) => {
  const api = await client()
  await call(api, 'post', '/auth/login', { username: 'mia', password: 'mias-password-1' })
  const token = (await call(api, 'post', '/account/tokens', { name: 'e2e-ci', scopes: ['read', 'write'] })).token as string
  await api.dispose()

  const dir = mkdtempSync(join(tmpdir(), 'tgp-e2e-ci-'))
  const url = new URL(server.url + '/game.git')
  url.username = 'mia'
  url.password = token
  git(dir, 'clone', '-q', url.toString(), 'game')
  const work = join(dir, 'game')
  mkdirSync(join(work, '.tgp/workflows'), { recursive: true })
  writeFileSync(
    join(work, '.tgp/workflows/ci.yml'),
    ['name: CI', 'on: [push, workflow_dispatch]', 'jobs:', '  test:', '    runs-on: linux', '    steps:', '      - uses: checkout', '      - run: echo hello', ''].join('\n'),
  )
  writeFileSync(join(work, '.tgp/workflows/broken.yml'), ['on: push', 'jobs:', '  a:', '    stepz: []', ''].join('\n'))
  git(work, 'add', '.')
  git(work, 'commit', '-qm', 'Add CI workflow')
  git(work, 'push', '-q', 'origin', 'HEAD')

  const mia = await loginAs(browser, 'mia', 'mias-password-1')
  await mia.goto(server.url + '/game/ci')
  // Each workflow gets its own run: the valid one waits for a runner, the
  // broken one fails straight away with its error.
  const row = mia.getByTestId('run-row').filter({ hasText: 'Add CI workflow' }).filter({ hasNotText: 'broken.yml' })
  await expect(row).toBeVisible()
  await expect(row).toContainText('CI')
  await expect(mia.getByTestId('run-row').filter({ hasText: 'broken.yml' })).toBeVisible()

  // The broken workflow is flagged with its errors.
  await mia.getByRole('button', { name: 'broken.yml has errors' }).click()
  const errs = mia.getByRole('dialog', { name: 'Errors in broken.yml' })
  await expect(errs).toContainText('stepz')
  await errs.getByRole('button', { name: 'Close' }).click()
  await shot(mia, 'ci-runs.png')

  // The run waits for a runner (none registered), then gets cancelled.
  await row.getByRole('link').click()
  await expect(mia.getByRole('heading', { name: 'Add CI workflow' })).toBeVisible()
  await expect(mia.getByRole('navigation', { name: 'Jobs' }).getByText('test')).toBeVisible()
  await expect(mia.getByText(/Waiting/).first()).toBeVisible()
  await shot(mia, 'ci-run-queued.png')
  await mia.getByRole('button', { name: 'Cancel run' }).click()
  await mia.getByRole('dialog').getByRole('button', { name: 'Cancel run' }).click()
  await expect(mia.getByText('Cancelled').first()).toBeVisible()
  await expect(mia.getByRole('button', { name: 'Cancel run' })).toHaveCount(0)
  await expect(mia.getByRole('button', { name: 'Re-run all jobs' })).toBeVisible()

  // The cross-repo CI page lists it.
  await mia.goto(server.url + '/ci')
  await expect(mia.getByTestId('run-row').filter({ hasText: 'Add CI workflow' }).filter({ hasNotText: 'broken.yml' })).toContainText('game')
  await mia.context().close()
})
