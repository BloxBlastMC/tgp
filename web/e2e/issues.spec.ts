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
    env: { ...process.env, HOME: cwd, GIT_CONFIG_NOSYSTEM: '1', GIT_AUTHOR_NAME: 'Mia', GIT_AUTHOR_EMAIL: 'mia@example.com', GIT_COMMITTER_NAME: 'Mia', GIT_COMMITTER_EMAIL: 'mia@example.com' },
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
  await expect(page.getByRole('heading', { name: 'Repositories' })).toBeVisible()
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

test('file, triage, filter, close and link issues', async ({ browser }) => {
  // Tom (tester) files a bug with a label.
  const tom = await loginAs(browser, 'tom', 'toms-password-1')
  await tom.goto(server.url + '/game/issues')
  await expect(tom.getByText('No open issues')).toBeVisible()
  await tom.getByRole('button', { name: 'New issue' }).click()
  await tom.getByLabel('Title').fill('Crash when joining the lobby')
  await tom.getByLabel('Description').fill('Happens every time on 1.20.')
  await tom.getByRole('button', { name: 'Edit labels' }).click()
  await tom.getByRole('option', { name: 'bug' }).click()
  await tom.getByRole('button', { name: 'Done' }).click()
  await expect(tom.getByLabel('Labels').getByText('bug')).toBeVisible()
  await tom.getByRole('button', { name: 'Create issue' }).click()
  await expect(tom).toHaveURL(/\/game\/issues\/1$/)
  await expect(tom.getByRole('heading', { name: /Crash when joining the lobby/ })).toBeVisible()
  await expect(tom.getByLabel('Labels').getByText('bug')).toBeVisible()

  // Mia (maintainer) creates a milestone and a label.
  const mia = await loginAs(browser, 'mia', 'mias-password-1')
  await mia.goto(server.url + '/game/milestones')
  await mia.getByRole('button', { name: 'New milestone' }).click()
  await mia.getByLabel('Title').fill('v1.0')
  await mia.getByLabel('Due date (optional)').fill('2030-01-31')
  await mia.getByRole('button', { name: 'Create milestone' }).click()
  await expect(mia.getByTestId('milestone-row')).toContainText('v1.0')
  await mia.goto(server.url + '/game/labels')
  await mia.getByRole('button', { name: 'New label' }).click()
  await mia.getByLabel('Name').fill('lobby')
  await mia.getByLabel('Description').fill('Lobby server')
  await mia.getByRole('button', { name: 'Create label' }).click()
  await expect(mia.getByTestId('label-row').filter({ hasText: 'lobby' })).toBeVisible()

  // Mia triages the issue from the sidebar: assignee, extra label, milestone.
  await mia.goto(server.url + '/game/issues/1')
  await mia.getByRole('button', { name: 'Edit assignees' }).click()
  await mia.getByRole('option', { name: 'Tom' }).click()
  await mia.getByRole('button', { name: 'Done' }).click()
  await expect(mia.getByLabel('Assignees').getByText('Tom')).toBeVisible()
  await mia.getByRole('button', { name: 'Edit labels' }).click()
  await mia.getByRole('option', { name: 'lobby' }).click()
  await mia.getByRole('button', { name: 'Done' }).click()
  await expect(mia.getByLabel('Labels').getByText('lobby')).toBeVisible()
  await mia.getByRole('button', { name: 'Edit milestone' }).click()
  await mia.getByRole('option', { name: 'v1.0' }).click()
  await expect(mia.locator('aside').getByRole('link', { name: 'v1.0' })).toBeVisible()
  await expect(mia.getByTestId('event').filter({ hasText: 'assigned tom' })).toBeVisible()
  await expect(mia.getByTestId('event').filter({ hasText: 'added this to the v1.0 milestone' })).toBeVisible()
  await shot(mia, 'i2-issue.png')

  // A second issue (via the API), then filter the list by label.
  const api = await client()
  await call(api, 'post', '/auth/login', { username: 'mia', password: 'mias-password-1' })
  await call(api, 'post', '/repos/game/issues', { title: 'Add a shop', labels: ['enhancement'] })
  await mia.goto(server.url + '/game/issues')
  await expect(mia.getByTestId('issue-row')).toHaveCount(2)
  await mia.getByTestId('issue-row').filter({ hasText: 'Crash' }).getByRole('button', { name: 'lobby' }).click()
  await expect(mia).toHaveURL(/labels=lobby/)
  await expect(mia.getByTestId('issue-row')).toHaveCount(1)
  await expect(mia.getByTestId('issue-row')).toContainText('Crash when joining the lobby')
  await mia.getByRole('button', { name: 'Clear filters' }).click()
  await expect(mia.getByTestId('issue-row')).toHaveCount(2)
  await shot(mia, 'i1-issues.png')

  // Close as not planned, then reopen.
  await mia.goto(server.url + '/game/issues/2')
  await mia.getByRole('button', { name: 'More ways to close' }).click()
  await mia.getByRole('menuitem', { name: /Close as not planned/ }).click()
  await expect(mia.getByText('Closed as not planned').first()).toBeVisible()
  await expect(mia.getByTestId('event').filter({ hasText: 'closed this as not planned' })).toBeVisible()
  await mia.getByRole('button', { name: 'Reopen issue' }).click()
  await expect(mia.getByTestId('event').filter({ hasText: 'reopened this' })).toBeVisible()

  // "#N" links: to another issue, and to a pull request (redirected).
  const token = (await call(api, 'post', '/account/tokens', { name: 'e2e', scopes: ['read', 'write'] })).token as string
  const url = new URL(server.url + '/game.git')
  url.username = 'mia'
  url.password = token
  const dir = mkdtempSync(join(tmpdir(), 'tgp-e2e-issues-'))
  git(dir, 'clone', '-q', url.toString(), 'game')
  const work = join(dir, 'game')
  git(work, 'checkout', '-q', '-b', 'fix-crash')
  writeFileSync(join(work, 'fix.txt'), 'fixed\n')
  git(work, 'add', '.')
  git(work, 'commit', '-q', '-m', 'Fix crash')
  git(work, 'push', '-q', 'origin', 'fix-crash')
  const pr = await call(api, 'post', '/repos/game/pulls', { title: 'Fix the lobby crash', head: 'fix-crash', body: 'Fixes #1' })
  expect(pr.number).toBe(3)

  await mia.goto(server.url + '/game/issues/1')
  await mia.getByLabel('Comment').fill('Related to #2, fix in #3.')
  await mia.getByRole('button', { name: 'Comment', exact: true }).click()
  const comment = mia.getByTestId('comment').filter({ hasText: 'Related to' })
  await expect(comment).toBeVisible()
  await comment.getByRole('link', { name: '#2' }).click()
  await expect(mia).toHaveURL(/\/game\/issues\/2$/)
  await expect(mia.getByRole('heading', { name: /Add a shop/ })).toBeVisible()
  await mia.goBack()
  await mia.getByTestId('comment').filter({ hasText: 'Related to' }).getByRole('link', { name: '#3' }).click()
  await expect(mia).toHaveURL(/\/game\/pulls\/3$/)
  await expect(mia.getByRole('heading', { name: /Fix the lobby crash/ })).toBeVisible()

  // The PR sidebar has the same pickers; labels show in the PR list.
  await mia.getByRole('button', { name: 'Edit labels' }).click()
  await mia.getByRole('option', { name: 'bug' }).click()
  await mia.getByRole('button', { name: 'Done' }).click()
  await expect(mia.getByLabel('Labels').getByText('bug')).toBeVisible()
  await mia.goto(server.url + '/game/pulls')
  await expect(mia.getByTestId('label').filter({ hasText: 'bug' })).toBeVisible()

  // Milestone page shows progress; global issues page shows Tom's assignment.
  await mia.goto(server.url + '/game/milestones')
  await expect(mia.getByTestId('milestone-row')).toContainText('1 open')
  await shot(mia, 'i3-milestones.png')
  await tom.goto(server.url + '/issues')
  await expect(tom.getByTestId('issue-row')).toContainText('Crash when joining the lobby')
  await api.dispose()
})
