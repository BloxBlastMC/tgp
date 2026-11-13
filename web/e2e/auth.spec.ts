import { expect, test, type Page } from '@playwright/test'
import { startServer, totp, type TestServer } from './server'

let server: TestServer
test.beforeAll(async () => {
  server = await startServer()
})
test.afterAll(async () => {
  await server?.stop()
})

async function signOut(page: Page) {
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login/)
}

test('setup wizard → temp password → forced change → 2FA → admin panel', async ({ page }) => {
  // A fresh server sends everyone to the setup wizard.
  await page.goto(server.url + '/')
  await expect(page).toHaveURL(/\/setup$/)

  // Step 1: a wrong token is caught at the end and sends you back.
  await page.getByLabel('Setup token').fill('wrong-token')
  await page.getByRole('button', { name: 'Continue' }).click()

  // Step 2: admin account
  await page.getByLabel('Username').fill('owen')
  await page.getByLabel('Display name').fill('Owen')
  await page.getByLabel('Password', { exact: true }).fill('owens-password-1')
  await page.getByLabel('Confirm password').fill('owens-password-1')
  await page.getByRole('button', { name: 'Continue' }).click()

  // Step 3: org
  await page.getByLabel('Organization name').fill('BloxBlast')
  await page.getByRole('button', { name: 'Continue' }).click()

  // Step 4: one team member
  await page.getByRole('button', { name: 'Add person' }).click()
  await page.getByLabel('Username').fill('alice')
  await page.getByLabel('Role').selectOption('tester')
  await page.getByRole('button', { name: 'Continue' }).click()

  // Step 5: review → submit with the wrong token
  await expect(page.getByText('alice')).toBeVisible()
  await page.getByRole('button', { name: 'Complete setup' }).click()
  await expect(page.getByRole('alert')).toContainText('invalid setup token')
  await expect(page.getByLabel('Setup token')).toBeVisible()
  await page.getByLabel('Setup token').fill(server.setupToken)
  for (let i = 0; i < 4; i++) await page.getByRole('button', { name: /Continue|Skip/ }).click()
  await page.getByRole('button', { name: 'Complete setup' }).click()

  // Done: alice's temp password is shown once.
  await expect(page.getByRole('heading', { name: "You're all set" })).toBeVisible()
  const temp = (await page.getByLabel('Temporary password for alice').textContent())!.trim()
  expect(temp).toMatch(/^[\w]{4}-[\w]{4}-[\w]{4}-[\w]{4}$/)
  await page.getByRole('button', { name: 'Continue to BloxBlast' }).click()
  await expect(page.getByRole('link', { name: /Admin/ })).toBeVisible()
  await signOut(page)

  // Alice logs in with the temp password and must change it.
  await page.getByLabel('Username or email').fill('alice')
  await page.getByLabel('Password').fill(temp)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/change-password$/)
  await page.goto(server.url + '/settings')
  await expect(page).toHaveURL(/\/change-password$/)
  await page.getByLabel('Temporary password').fill(temp)
  await page.getByLabel('New password', { exact: true }).fill('alices-password-1')
  await page.getByLabel('Confirm new password').fill('alices-password-1')
  await page.getByRole('button', { name: 'Set password' }).click()

  // Optional 2FA enrollment.
  await page.getByRole('button', { name: 'Set up two-factor authentication' }).click()
  await expect(page.getByAltText('2FA QR code')).toBeVisible()
  const secret = (await page.getByLabel('TOTP secret').textContent())!.trim()
  await page.getByLabel('Authentication code').fill(totp(secret))
  await page.getByRole('button', { name: 'Turn on 2FA' }).click()
  await expect(page.getByText('Two-factor authentication is on.')).toBeVisible()
  await page.getByRole('button', { name: "I've saved them" }).click()

  // Alice is a tester: no admin link, and /admin bounces.
  await expect(page.getByRole('heading', { name: 'Repositories' })).toBeVisible()
  await expect(page.getByRole('link', { name: /Admin/ })).toHaveCount(0)
  await page.goto(server.url + '/admin/users')
  await expect(page).toHaveURL(server.url + '/')
  await signOut(page)

  // Logging in again needs the 2FA code (next time step: the enroll code was consumed).
  await page.getByLabel('Username or email').fill('alice')
  await page.getByLabel('Password').fill('alices-password-1')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.getByLabel('Authentication code').fill(totp(secret, Date.now() + 30_000))
  await page.getByRole('button', { name: 'Verify' }).click()
  await expect(page.getByRole('heading', { name: 'Repositories' })).toBeVisible()
  await signOut(page)

  // Admin creates another user from the admin panel.
  await page.getByLabel('Username or email').fill('owen')
  await page.getByLabel('Password').fill('owens-password-1')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.getByRole('link', { name: /Admin/ }).click()
  await expect(page).toHaveURL(/\/admin\/users$/)
  await expect(page.getByRole('row', { name: /alice/ }).getByText('2FA')).toBeVisible()
  await page.getByRole('button', { name: 'New user' }).click()
  await page.getByLabel('Username').fill('bob')
  await page.getByLabel('Role').selectOption('maintainer')
  await page.getByRole('button', { name: 'Create user' }).click()
  await expect(page.getByRole('dialog', { name: 'Temporary password' }).getByLabel('Temporary password')).toBeVisible()
  await page.getByRole('button', { name: 'Done' }).click()
  await expect(page.getByRole('row', { name: /bob/ }).getByText('temp password')).toBeVisible()

  // The audit log recorded it all.
  await page.getByRole('link', { name: 'Audit log' }).click()
  await expect(page.getByText('setup.complete')).toBeVisible()
  await expect(page.getByText('account.totp_enable')).toBeVisible()

  // Command palette navigation.
  await page.keyboard.press('Control+k')
  await page.getByLabel('Command', { exact: true }).fill('settings')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/settings$/)
})
