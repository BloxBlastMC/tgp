import { expect, request, test, type APIRequestContext } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { startServer, type TestServer } from './server'

let server: TestServer
let api: APIRequestContext
let token = ''

async function csrf() {
  return (await api.storageState()).cookies.find((c) => c.name === 'tgp_csrf')?.value ?? ''
}

async function post(path: string, data: unknown) {
  const r = await api.post('/api/v1' + path, { data, headers: { 'X-CSRF-Token': await csrf() } })
  if (!r.ok()) throw new Error(`${path}: ${r.status()} ${await r.text()}`)
  return r.json()
}

function git(cwd: string, ...args: string[]) {
  return execFileSync('git', args, {
    cwd,
    env: { ...process.env, HOME: cwd, GIT_CONFIG_NOSYSTEM: '1', GIT_AUTHOR_NAME: 'Owen', GIT_AUTHOR_EMAIL: 'owen@example.com', GIT_COMMITTER_NAME: 'Owen', GIT_COMMITTER_EMAIL: 'owen@example.com' },
  }).toString()
}

test.beforeAll(async () => {
  server = await startServer()
  api = await request.newContext({ baseURL: server.url })
  await api.get('/api/v1/meta')
  await post('/setup', {
    setup_token: server.setupToken,
    admin: { username: 'owen', password: 'owens-password-1' },
    org: { name: 'BloxBlast' },
    users: [{ username: 'mia', role: 'maintainer' }],
  })
  token = (await post('/account/tokens', { name: 'e2e', scopes: ['read', 'write'] })).token
})

test.afterAll(async () => {
  await api?.dispose()
  await server?.stop()
})

test('create, push, browse, history, blame, settings and access', async ({ page }) => {
  await page.goto(server.url + '/login')
  await page.getByLabel('Username or email').fill('owen')
  await page.getByLabel('Password').fill('owens-password-1')
  await page.getByRole('button', { name: 'Sign in' }).click()

  // Create an empty repository from the UI.
  await expect(page.getByText('No repositories yet')).toBeVisible()
  await page.getByRole('button', { name: 'Create the first repository' }).click()
  await page.getByLabel('Name').fill('skywars')
  await page.getByLabel('Description').fill('Skywars minigame')
  await page.getByLabel('Initialize with a README').uncheck()
  await page.getByRole('button', { name: 'Create repository' }).click()
  await expect(page.getByText('This repository is empty')).toBeVisible()
  const cloneUrl = (await page.locator('code').first().textContent())!.trim()
  expect(cloneUrl).toBe(`${server.url}/skywars.git`)

  // Push real commits with git.
  const dir = mkdtempSync(join(tmpdir(), 'tgp-e2e-git-'))
  git(dir, 'init', '-q', '-b', 'main')
  mkdirSync(join(dir, 'src'))
  writeFileSync(join(dir, 'README.md'), '# Skywars\n\nIslands in the sky. See [the plugin](src/Plugin.java).\n')
  writeFileSync(join(dir, 'src/Plugin.java'), 'public class Plugin {\n    void onEnable() {}\n}\n')
  git(dir, 'add', '-A')
  git(dir, 'commit', '-q', '-m', 'Initial plugin')
  writeFileSync(join(dir, 'src/Plugin.java'), 'public class Plugin {\n    void onEnable() {\n        getLogger().info("hi");\n    }\n}\n')
  git(dir, 'commit', '-qam', 'Log on enable')
  const url = new URL(cloneUrl)
  url.username = 'owen'
  url.password = token
  git(dir, 'push', '-q', url.toString(), 'main')

  // Code view with README.
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Skywars' })).toBeVisible()
  await expect(page.getByText('Log on enable')).toBeVisible()
  await page.getByRole('link', { name: 'src' }).click()
  await page.getByRole('link', { name: 'Plugin.java' }).click()
  await expect(page.getByText('5 lines')).toBeVisible()
  // Syntax highlighting loads lazily.
  await expect(page.locator('.code-line span[style]').first()).toBeVisible()
  // Line anchors.
  await page.getByRole('cell', { name: '3', exact: true }).click()
  expect(page.url()).toContain('#L3')

  // Blame.
  await page.getByRole('button', { name: 'Blame' }).click()
  await expect(page.getByRole('link', { name: 'Initial plugin' })).toHaveCount(2) // lines 1-2 and the closing braces
  await expect(page.getByRole('link', { name: 'Log on enable' }).first()).toBeVisible()

  // History → commit diff.
  await page.getByRole('link', { name: 'Commits' }).click()
  await page.getByRole('link', { name: 'Log on enable' }).first().click()
  await expect(page.getByText('1 file changed')).toBeVisible()
  await expect(page.getByText('getLogger().info("hi");')).toBeVisible()
  await page.getByRole('button', { name: 'Split' }).click()
  await expect(page.getByText('getLogger().info("hi");')).toBeVisible()

  // Grant mia this repository from the admin panel.
  await page.goto(server.url + '/admin/users')
  await page.getByRole('button', { name: 'Edit mia' }).click()
  await page.getByLabel('Access to all repositories').uncheck()
  await page.getByRole('dialog').getByText('skywars').click()
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByRole('dialog', { name: 'Edit mia' })).toBeHidden()
  await page.goto(server.url + '/skywars/settings')
  await expect(page.getByRole('listitem').filter({ hasText: 'mia' }).getByText('Granted')).toBeVisible()

  // Rename, then delete with confirmation.
  await page.getByLabel('Rename').fill('skywars-legacy')
  await page.getByRole('button', { name: 'Rename' }).click()
  await expect(page).toHaveURL(/\/skywars-legacy\/settings$/)
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  await page.getByLabel('Repository name').fill('skywars-legacy')
  await page.getByRole('button', { name: 'Delete forever' }).click()
  await expect(page.getByText('No repositories yet')).toBeVisible()
})
