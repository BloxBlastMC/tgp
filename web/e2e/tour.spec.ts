import { request, test, type APIRequestContext, type Browser, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { createHash, randomBytes } from 'node:crypto'
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { startServer, type TestServer } from './server'

// A visual tour: seeds a realistic server and screenshots every page in
// both themes. Runs only with TGP_SHOTS=<dir>; it asserts nothing.
//   TGP_SHOTS=/tmp/shots pnpm e2e tour
const shots = process.env.TGP_SHOTS
test.skip(!shots, 'set TGP_SHOTS to take tour screenshots')

let server: TestServer

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

/** Pushes a small OCI image (or a two-platform index) to the registry with Basic auth. */
async function pushImage(path: string, tag: string, token: string, platforms?: string[]) {
  const auth = 'Basic ' + Buffer.from('mia:' + token).toString('base64')
  const sha = (b: Buffer) => 'sha256:' + createHash('sha256').update(b).digest('hex')
  const blob = async (b: Buffer) => {
    const d = sha(b)
    const r = await fetch(`${server.url}/v2/${path}/blobs/uploads/?digest=${d}`, { method: 'POST', body: new Uint8Array(b), headers: { Authorization: auth } })
    if (r.status !== 201) throw new Error(`blob: ${r.status} ${await r.text()}`)
    return { digest: d, size: b.length }
  }
  // ref undefined pushes by digest (an index's children).
  const manifest = async (ref: string | undefined, mediaType: string, body: unknown) => {
    const b = Buffer.from(JSON.stringify(body))
    const r = await fetch(`${server.url}/v2/${path}/manifests/${ref ?? sha(b)}`, { method: 'PUT', body: new Uint8Array(b), headers: { Authorization: auth, 'Content-Type': mediaType } })
    if (r.status !== 201) throw new Error(`manifest: ${r.status} ${await r.text()}`)
    return { digest: sha(b), size: b.length }
  }
  const image = async (ref: string | undefined, os: string, arch: string) => {
    const config = await blob(Buffer.from(JSON.stringify({ architecture: arch, os, rootfs: { type: 'layers', diff_ids: [] } })))
    const layers = [await blob(randomBytes(180_000)), await blob(randomBytes(40_000))]
    return manifest(ref, 'application/vnd.oci.image.manifest.v1+json', {
      schemaVersion: 2,
      mediaType: 'application/vnd.oci.image.manifest.v1+json',
      config: { mediaType: 'application/vnd.oci.image.config.v1+json', ...config },
      layers: layers.map((l) => ({ mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip', ...l })),
    })
  }
  if (!platforms) return image(tag, 'linux', 'amd64')
  const children = []
  for (const p of platforms) {
    const [os, arch] = p.split('/')
    children.push({ ...(await image(undefined, os, arch)), platform: { os, architecture: arch } })
  }
  return manifest(tag, 'application/vnd.oci.image.index.v1+json', {
    schemaVersion: 2,
    mediaType: 'application/vnd.oci.image.index.v1+json',
    manifests: children.map((c) => ({ mediaType: 'application/vnd.oci.image.manifest.v1+json', digest: c.digest, size: c.size, platform: c.platform })),
  })
}

function git(cwd: string, ...args: string[]) {
  return execFileSync('git', args, {
    cwd,
    env: { ...process.env, HOME: cwd, GIT_CONFIG_NOSYSTEM: '1', GIT_AUTHOR_NAME: 'Mia Chen', GIT_AUTHOR_EMAIL: 'mia@example.com', GIT_COMMITTER_NAME: 'Mia Chen', GIT_COMMITTER_EMAIL: 'mia@example.com' },
  }).toString()
}

async function login(browser: Browser, theme: 'dark' | 'light', username: string, password: string): Promise<Page> {
  const ctx = await browser.newContext({ colorScheme: theme, viewport: { width: 1440, height: 900 } })
  const page = await ctx.newPage()
  await page.goto(server.url + '/login')
  await page.getByLabel('Username or email').fill(username)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.getByRole('heading', { name: 'Home', exact: true }).waitFor()
  return page
}

async function snap(page: Page, theme: string, name: string, path?: string) {
  if (path) await page.goto(server.url + path)
  await page.waitForLoadState('networkidle')
  await page.waitForTimeout(250)
  const dir = join(shots!, theme)
  mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: join(dir, `${name}.png`) })
}

test.beforeAll(async () => {
  test.setTimeout(120_000)
  server = await startServer()
})

test.afterAll(async () => {
  await server?.stop()
})

test('tour', async ({ browser }) => {
  test.setTimeout(300_000)
  // Setup wizard before anything exists.
  for (const theme of ['dark', 'light'] as const) {
    const ctx = await browser.newContext({ colorScheme: theme, viewport: { width: 1440, height: 900 } })
    const p = await ctx.newPage()
    await p.goto(server.url + '/setup')
    await snap(p, theme, '00-setup')
    await ctx.close()
  }

  const admin = await client()
  const setup = await call(admin, 'post', '/setup', {
    setup_token: server.setupToken,
    admin: { username: 'owen', password: 'owens-password-1', display_name: 'Owen Park', email: 'owen@bloxblast.net' },
    org: { name: 'BloxBlast' },
    users: [
      { username: 'mia', display_name: 'Mia Chen', email: 'mia@bloxblast.net', role: 'maintainer', all_repos: true },
      { username: 'tom', display_name: 'Tom Reyes', email: 'tom@bloxblast.net', role: 'tester', all_repos: true },
      { username: 'ava', display_name: 'Ava Singh', role: 'maintainer', all_repos: true },
    ],
  })
  for (const u of setup.users as { user: { username: string }; temp_password: string }[]) {
    if (u.user.username === 'ava') continue
    const c = await client()
    await call(c, 'post', '/auth/login', { username: u.user.username, password: u.temp_password })
    await call(c, 'post', '/account/password', { current_password: u.temp_password, new_password: `${u.user.username}s-password-1` })
    await c.dispose()
  }
  await call(admin, 'post', '/auth/login', { username: 'owen', password: 'owens-password-1' })
  await call(admin, 'post', '/repos', { name: 'game', description: 'Core minigame server plugin', auto_init: true })
  await call(admin, 'post', '/repos', { name: 'lobby', description: 'Lobby world, NPCs and server selector', auto_init: true })
  await call(admin, 'post', '/repos', { name: 'website', description: 'bloxblast.net marketing site', auto_init: true })
  await call(admin, 'post', '/repos', { name: 'infra', description: 'Deployment configs', auto_init: false })

  // Mia pushes code, a workflow and a feature branch.
  const mia = await client()
  await call(mia, 'post', '/auth/login', { username: 'mia', password: 'mias-password-1' })
  const token = (await call(mia, 'post', '/account/tokens', { name: 'laptop', scopes: ['read', 'write'] })).token as string
  const url = new URL(server.url + '/game.git')
  url.username = 'mia'
  url.password = token
  const dir = mkdtempSync(join(tmpdir(), 'tgp-tour-'))
  git(dir, 'clone', '-q', url.toString(), 'game')
  const work = join(dir, 'game')
  mkdirSync(join(work, 'src/main/java/net/bloxblast'), { recursive: true })
  mkdirSync(join(work, '.tgp/workflows'), { recursive: true })
  writeFileSync(
    join(work, 'README.md'),
    '# Game\n\nThe core minigame plugin for **BloxBlast**: Skywars, Bedwars and Duels.\n\n## Building\n\n```sh\n./gradlew build\n```\n\n| Mode | Players |\n| --- | --- |\n| Skywars | 2–12 |\n| Bedwars | 4–16 |\n',
  )
  writeFileSync(
    join(work, 'src/main/java/net/bloxblast/GamePlugin.java'),
    'package net.bloxblast;\n\nimport org.bukkit.plugin.java.JavaPlugin;\n\npublic final class GamePlugin extends JavaPlugin {\n    @Override\n    public void onEnable() {\n        getLogger().info("Game enabled");\n        saveDefaultConfig();\n    }\n\n    @Override\n    public void onDisable() {\n        getLogger().info("Game disabled");\n    }\n}\n',
  )
  writeFileSync(join(work, 'build.gradle'), "plugins { id 'java' }\n\ngroup = 'net.bloxblast'\nversion = '1.4.0'\n")
  writeFileSync(join(work, '.tgp/workflows/ci.yml'), ['name: CI', 'on: [push, workflow_dispatch]', 'jobs:', '  build:', '    runs-on: linux', '    steps:', '      - uses: checkout', '      - run: ./gradlew build', ''].join('\n'))
  git(work, 'add', '-A')
  git(work, 'commit', '-q', '-m', 'Add the plugin skeleton and CI')
  writeFileSync(join(work, 'build.gradle'), "plugins { id 'java' }\n\ngroup = 'net.bloxblast'\nversion = '1.4.1'\n")
  git(work, 'commit', '-qam', 'Bump version to 1.4.1')
  git(work, 'push', '-q', 'origin', 'HEAD')
  git(work, 'checkout', '-q', '-b', 'feature/scoreboard')
  writeFileSync(join(work, 'src/main/java/net/bloxblast/Scoreboard.java'), 'package net.bloxblast;\n\npublic final class Scoreboard {\n    private int kills;\n\n    public void addKill() {\n        kills++;\n    }\n\n    public int kills() {\n        return kills;\n    }\n}\n')
  git(work, 'add', '-A')
  git(work, 'commit', '-q', '-m', 'Add a per-player scoreboard')
  git(work, 'push', '-q', 'origin', 'feature/scoreboard')

  // Container images: commit tags, a moving tag and a multi-platform image.
  await pushImage('bloxblast/game/server', 'g1a2b3c4d5e6f', token)
  await pushImage('bloxblast/game/server', 'g7f8e9d0c1b2a', token)
  await pushImage('bloxblast/game/server', 'master', token)
  await pushImage('bloxblast/game/proxy', 'v2', token, ['linux/amd64', 'linux/arm64'])

  for (const l of [
    { name: 'bug', color: 'd73a4a', description: "Something isn't working" },
    { name: 'enhancement', color: 'a2eeef', description: 'New feature or request' },
    { name: 'lobby', color: '7057ff', description: 'Lobby world' },
    { name: 'priority', color: 'fbca04', description: 'Needs attention soon' },
  ])
    await call(mia, 'post', '/repos/game/labels', l).catch(() => {}) // defaults may exist
  const ms = await call(mia, 'post', '/repos/game/milestones', { title: 'v1.5', description: 'Season 2 launch', due_on: '2030-01-31' })
  await call(mia, 'post', '/repos/game/milestones', { title: 'v1.4.2', description: 'Hotfixes', due_on: '2026-11-01' })
  await call(mia, 'post', '/repos/game/issues', { title: 'Players fall through the map in Skywars', body: 'Happens on **island 3** after respawn.', labels: ['bug', 'priority'], assignees: ['owen'], milestone: ms.id })
  await call(mia, 'post', '/repos/game/issues', { title: 'Add a shop to the lobby', labels: ['enhancement', 'lobby'], assignees: ['owen', 'mia'] })
  await call(mia, 'post', '/repos/game/issues', { title: 'Kill feed overlaps the scoreboard', labels: ['bug'], milestone: ms.id })
  const closed = await call(mia, 'post', '/repos/game/issues', { title: 'Typo in welcome message' })
  await call(mia, 'patch', `/repos/game/issues/${closed.number}`, { state: 'closed' })
  const pr = await call(mia, 'post', '/repos/game/pulls', {
    title: 'Add a per-player scoreboard',
    head: 'feature/scoreboard',
    body: 'Tracks kills per player.\n\n- [x] Scoreboard class\n- [ ] Wire into game loop',
    reviewers: ['owen', 'tom'],
    labels: ['enhancement'],
    milestone: ms.id,
  })
  await call(mia, 'post', `/repos/game/pulls/${pr.number}/comments`, { body: 'Should this reset between rounds?', path: 'src/main/java/net/bloxblast/Scoreboard.java', line: 4 })
  await call(mia, 'post', `/repos/game/pulls/${pr.number}/comments`, { body: 'Ready for a look — the wiring comes in a follow-up.' })
  await call(mia, 'post', '/repos/game/releases', { tag: 'v1.4.0', name: 'Season 1.4', body: '## Highlights\n\n- New Duels mode\n- Faster joins' })
  await call(mia, 'post', '/repos/game/webhooks', { url: 'https://hooks.example.com/tgp', events: ['push', 'pull_request'], secret: 's3cret' })
  await mia.dispose()
  await admin.dispose()

  for (const theme of ['dark', 'light'] as const) {
    const p = await login(browser, theme, 'owen', 'owens-password-1')
    await snap(p, theme, '01-home')
    await snap(p, theme, '02-repos', '/repos')
    await snap(p, theme, '03-code', '/game')
    await snap(p, theme, '04-blob', '/game/blob?path=src/main/java/net/bloxblast/GamePlugin.java')
    await snap(p, theme, '05-commits', '/game/commits')
    await snap(p, theme, '06-branches', '/game/branches')
    await snap(p, theme, '07-empty-repo', '/infra')
    await snap(p, theme, '10-issues', '/game/issues')
    await snap(p, theme, '11-issue', '/game/issues/1')
    await snap(p, theme, '12-labels', '/game/labels')
    await snap(p, theme, '13-milestones', '/game/milestones')
    await snap(p, theme, '14-new-issue', '/game/issues/new')
    await snap(p, theme, '15-global-issues', '/issues')
    await snap(p, theme, '20-pulls', '/game/pulls')
    await snap(p, theme, '21-pull', `/game/pulls/${pr.number}`)
    await snap(p, theme, '22-pull-files', `/game/pulls/${pr.number}?tab=files`)
    await snap(p, theme, '23-compare', '/game/compare/main...feature%2Fscoreboard')
    await snap(p, theme, '24-global-pulls', '/pulls')
    await snap(p, theme, '30-runs', '/game/ci')
    await snap(p, theme, '31-run', '/game/ci/runs/1')
    await snap(p, theme, '32-caches', '/game/ci/caches')
    await snap(p, theme, '33-global-runs', '/ci')
    await snap(p, theme, '34-packages', '/game/packages')
    await snap(p, theme, '35-package', '/game/packages/image?name=server')
    await snap(p, theme, '36-package-index', '/game/packages/image?name=proxy')
    await snap(p, theme, '40-releases', '/game/releases')
    await snap(p, theme, '41-release-new', '/game/releases/new')
    await snap(p, theme, '42-repo-settings', '/game/settings')
    await snap(p, theme, '43-webhooks', '/game/settings/webhooks')
    await snap(p, theme, '44-webhook', '/game/settings/webhooks/1')
    await snap(p, theme, '50-admin-users', '/admin/users')
    await snap(p, theme, '51-admin-health', '/admin/health')
    await snap(p, theme, '52-admin-org', '/admin/org')
    await snap(p, theme, '53-admin-runners', '/admin/runners')
    await snap(p, theme, '54-admin-ci', '/admin/ci')
    await snap(p, theme, '57-admin-registry', '/admin/registry')
    await snap(p, theme, '55-admin-audit', '/admin/audit')
    await snap(p, theme, '56-admin-webhooks', '/admin/webhooks')
    await snap(p, theme, '60-settings', '/settings')
    await p.goto(server.url + '/')
    await p.keyboard.press('Control+k')
    await snap(p, theme, '61-palette')
    await p.keyboard.press('Escape')
    await p.getByRole('button', { name: 'Account menu' }).click()
    await snap(p, theme, '62-account-menu')
    await p.keyboard.press('Escape')
    await snap(p, theme, '63-not-found', '/game/nope/nope')
    await p.context().close()
    const ctx = await browser.newContext({ colorScheme: theme, viewport: { width: 1440, height: 900 } })
    const lp = await ctx.newPage()
    await snap(lp, theme, '64-login', '/login')
    await ctx.close()
  }

  // A narrower laptop width for the densest pages.
  const narrow = await login(browser, 'dark', 'owen', 'owens-password-1')
  await narrow.setViewportSize({ width: 1024, height: 768 })
  for (const [name, path] of [
    ['70-narrow-home', '/'],
    ['71-narrow-issues', '/game/issues'],
    ['72-narrow-pull', `/game/pulls/${pr.number}`],
    ['73-narrow-users', '/admin/users'],
    ['74-narrow-run', '/game/ci/runs/1'],
  ] as const)
    await snap(narrow, 'dark', name, path)
  await narrow.context().close()
})
