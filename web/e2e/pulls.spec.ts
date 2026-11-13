import { expect, request, test, type APIRequestContext, type Browser, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { startServer, type TestServer } from './server'

let server: TestServer
const shots = process.env.TGP_SHOTS // directory for screenshots, optional

async function client(): Promise<APIRequestContext> {
  const c = await request.newContext({ baseURL: server.url })
  await c.get('/api/v1/meta')
  return c
}

async function call(c: APIRequestContext, method: 'post' | 'put', path: string, data: unknown) {
  const csrf = (await c.storageState()).cookies.find((k) => k.name === 'tgp_csrf')?.value ?? ''
  const r = await c[method]('/api/v1' + path, { data, headers: { 'X-CSRF-Token': csrf } })
  if (!r.ok()) throw new Error(`${path}: ${r.status()} ${await r.text()}`)
  return r.status() === 204 ? null : r.json()
}

function git(cwd: string, ...args: string[]) {
  return execFileSync('git', args, {
    cwd,
    env: { ...process.env, HOME: cwd, GIT_CONFIG_NOSYSTEM: '1', GIT_AUTHOR_NAME: 'Mia', GIT_AUTHOR_EMAIL: 'mia@example.com', GIT_COMMITTER_NAME: 'Mia', GIT_COMMITTER_EMAIL: 'mia@example.com' },
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
  // Activate the users with real passwords.
  for (const u of setup.users as { user: { username: string }; temp_password: string }[]) {
    const c = await client()
    await call(c, 'post', '/auth/login', { username: u.user.username, password: u.temp_password })
    await call(c, 'post', '/account/password', { current_password: u.temp_password, new_password: `${u.user.username}s-password-1` })
    await c.dispose()
  }
  await call(admin, 'post', '/repos', { name: 'game', auto_init: true })

  // Mia pushes a feature branch.
  const mia = await client()
  await call(mia, 'post', '/auth/login', { username: 'mia', password: 'mias-password-1' })
  const token = (await call(mia, 'post', '/account/tokens', { name: 'e2e', scopes: ['read', 'write'] })).token as string
  const url = new URL(server.url + '/game.git')
  url.username = 'mia'
  url.password = token
  const dir = mkdtempSync(join(tmpdir(), 'tgp-e2e-pr-'))
  git(dir, 'clone', '-q', url.toString(), 'game')
  const work = join(dir, 'game')
  git(work, 'checkout', '-q', '-b', 'feature/scores')
  writeFileSync(join(work, 'scores.txt'), 'alice 10\nbob 7\ncarol 3\n')
  git(work, 'add', '.')
  git(work, 'commit', '-q', '-m', 'Add a scoreboard')
  const out = execFileSync('git', ['push', 'origin', 'feature/scores'], { cwd: work, env: { ...process.env, HOME: work }, stdio: ['ignore', 'pipe', 'pipe'] })
  void out
  await mia.dispose()
  await admin.dispose()
})

test.afterAll(async () => {
  await server?.stop()
})

test('open, review, protect and merge a pull request', async ({ browser }) => {
  // Mia opens the PR from the compare URL git prints after a push.
  const mia = await loginAs(browser, 'mia', 'mias-password-1')
  await mia.goto(server.url + '/game/compare/main...feature%2Fscores')
  await expect(mia.getByRole('heading', { name: 'Open a pull request' })).toBeVisible()
  await expect(mia.getByText('Can be merged.')).toBeVisible()
  await expect(mia.getByLabel('Title')).toHaveValue('Add a scoreboard')
  await mia.getByLabel('Description').fill('Adds **scores** for the lobby.')
  await mia.getByRole('button', { name: 'Edit reviewers' }).click()
  await mia.getByRole('option', { name: /Tom/ }).click()
  await mia.getByRole('button', { name: 'Done' }).click()
  await expect(mia.getByRole('complementary').getByText('Tom')).toBeVisible()
  await shot(mia, 'p1-compare.png')
  await mia.getByRole('button', { name: 'Create pull request' }).click()
  await expect(mia).toHaveURL(/\/game\/pulls\/1$/)
  await expect(mia.getByRole('heading', { name: /Add a scoreboard/ })).toBeVisible()
  await expect(mia.locator('.markdown strong', { hasText: 'scores' })).toBeVisible()

  // Tom sees the review request, comments on a line and requests changes.
  const tom = await loginAs(browser, 'tom', 'toms-password-1')
  await expect(tom.getByLabel('1 waiting for your review')).toBeVisible()
  await tom.getByRole('link', { name: /Pull requests/ }).first().click()
  await tom.getByRole('link', { name: /Add a scoreboard/ }).click()
  await tom.getByRole('link', { name: /Files changed/ }).click()
  const line = tom.getByRole('row').filter({ hasText: 'bob 7' })
  await line.hover()
  await tom.getByRole('button', { name: 'Comment on scores.txt line 2' }).click()
  await tom.getByLabel('Line comment').fill('Bob had 8 points.')
  await tom.getByRole('button', { name: 'Start a review' }).click()
  await expect(tom.getByText('Part of your review')).toBeVisible()
  await tom.getByRole('button', { name: 'Review (1 pending)' }).click()
  await tom.getByRole('dialog').getByLabel('Review summary').fill('One fix needed.')
  await tom.getByRole('dialog').getByText('Request changes').click()
  await tom.getByRole('button', { name: 'Submit review' }).click()
  await expect(tom.getByTestId('review').filter({ hasText: 'requested changes' })).toBeVisible()
  await expect(tom.getByTestId('thread').getByText('Bob had 8 points.')).toBeVisible()
  await expect(tom.getByRole('link', { name: /Pull requests/ }).first().getByLabel(/waiting for your review/)).toHaveCount(0)

  // Mia replies and resolves; Tom then approves.
  await mia.reload()
  await mia.getByTestId('thread').getByRole('button', { name: 'Reply…' }).click()
  await mia.getByLabel('Reply').fill('Fixed in the data source.')
  await mia.getByRole('button', { name: 'Reply', exact: true }).click()
  await expect(mia.getByText('Fixed in the data source.')).toBeVisible()
  await mia.getByTestId('thread').getByRole('button', { name: 'Resolve' }).click()
  await expect(mia.getByTestId('thread').getByText('resolved')).toBeVisible()

  await tom.reload()
  await tom.getByRole('button', { name: /^Review/ }).click()
  await tom.getByRole('dialog').getByText('Approve', { exact: true }).click()
  await tom.getByRole('button', { name: 'Submit review' }).click()
  await expect(tom.getByTestId('review').filter({ hasText: 'approved these changes' })).toBeVisible()

  // Owen protects main from the repository settings.
  const owen = await loginAs(browser, 'owen', 'owens-password-1')
  await owen.goto(server.url + '/game/settings')
  await owen.getByRole('button', { name: 'Add rule' }).click()
  await owen.getByLabel('Branch name or pattern').fill('main')
  await expect(owen.getByLabel('Required approvals')).toHaveValue('1')
  await owen.getByRole('dialog').getByRole('button', { name: 'Add rule' }).click()
  await expect(owen.getByRole('list', { name: 'Protection rules' }).getByText('pull requests only')).toBeVisible()
  await owen.goto(server.url + '/game/branches')
  await expect(owen.getByRole('listitem').filter({ hasText: 'main' }).getByText('protected')).toBeVisible()

  // Mia squash-merges and the branch is deleted.
  await mia.reload()
  await expect(mia.getByRole('complementary').getByText('main', { exact: true })).toBeVisible()
  await expect(mia.getByText('This branch has no conflicts with main.')).toBeVisible()
  await mia.getByLabel('Merge method').selectOption('squash')
  await expect(mia.getByLabel('Commit title')).toHaveValue('Add a scoreboard (#1)')
  await shot(mia, 'p2-conversation.png')
  await mia.getByRole('button', { name: 'Squash and merge' }).click()
  await expect(mia.getByRole('paragraph').filter({ hasText: /^Merged$/ })).toBeVisible()
  await expect(mia.getByText('Branch feature/scores deleted')).toBeVisible()
  await expect(mia.getByTestId('event').filter({ hasText: 'merged this (squash)' })).toBeVisible()
  await expect(mia.getByTestId('event').filter({ hasText: 'deleted the branch feature/scores' })).toBeVisible()

  // The list shows it as merged.
  await mia.goto(server.url + '/game/pulls?state=merged')
  await expect(mia.getByRole('link', { name: /Add a scoreboard/ })).toBeVisible()
  await mia.getByRole('link', { name: /Add a scoreboard/ }).click()
  await mia.getByRole('link', { name: /Files changed/ }).click()
  // Resolved threads start collapsed next to their line.
  await expect(mia.getByTestId('thread').getByText('Resolved by Mia')).toBeVisible()
  await mia.getByTestId('thread').getByRole('button', { name: 'Show' }).click()
  await expect(mia.getByText('Bob had 8 points.')).toBeVisible()
  await shot(mia, 'p3-files.png')
})
