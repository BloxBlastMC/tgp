// Human labels for identifiers the server sends (health checks, audit
// actions, webhook events). Unknown keys fall back to a readable form.

/** "ci_queue" → "Ci queue", "account.password_change" → "Account password change". */
export function humanize(key: string): string {
  const s = key.replace(/[._-]+/g, ' ').trim()
  return s.charAt(0).toUpperCase() + s.slice(1)
}

const healthChecks: Record<string, string> = {
  runners: 'Runners',
  database: 'Database',
  migrations: 'Migrations',
  storage: 'Storage',
  disk: 'Disk space',
  master_key: 'Master key',
  ci_queue: 'CI queue',
  webhooks: 'Webhooks',
  registry: 'Container registry',
  git: 'Git',
}

export function healthCheckLabel(name: string): string {
  return healthChecks[name] ?? humanize(name)
}

const auditActions: Record<string, string> = {
  'auth.login': 'Signed in',
  'auth.logout': 'Signed out',
  'auth.login_failed': 'Sign-in failed',
  'auth.totp_failed': 'Two-factor code rejected',
  'account.password_change': 'Password changed',
  'account.totp_enable': 'Two-factor turned on',
  'account.totp_disable': 'Two-factor turned off',
  'account.recovery_codes': 'Recovery codes regenerated',
  'token.create': 'Token created',
  'token.revoke': 'Token revoked',
  'token.delete': 'Token revoked',
  'setup.complete': 'Setup completed',
  'user.create': 'User created',
  'user.update': 'User updated',
  'user.delete': 'User deleted',
  'user.disable': 'User disabled',
  'user.enable': 'User enabled',
  'user.reset_password': 'Password reset',
  'user.access': 'Access changed',
  'org.update': 'Organization updated',
  'org.logo': 'Logo changed',
  'repo.create': 'Repository created',
  'repo.update': 'Repository updated',
  'repo.rename': 'Repository renamed',
  'repo.archive': 'Repository archived',
  'repo.unarchive': 'Repository unarchived',
  'repo.delete': 'Repository deleted',
  'webhook.create': 'Webhook created',
  'webhook.update': 'Webhook updated',
  'webhook.delete': 'Webhook deleted',
  'runner.register': 'Runner registered',
  'runner.delete': 'Runner removed',
  'protection.create': 'Branch protection added',
  'protection.update': 'Branch protection changed',
  'protection.delete': 'Branch protection removed',
  'protection.bypass': 'Branch protection bypassed',
  'secret.set': 'Secret saved',
  'secret.delete': 'Secret deleted',
}

export function auditActionLabel(action: string): string {
  return auditActions[action] ?? humanize(action)
}

/**
 * Audit tone: red for deletions and bypasses, amber for admin-level
 * changes (users, org, setup), neutral for everything else.
 */
export function auditActionTone(action: string): 'danger' | 'warning' | 'neutral' {
  if (/\.(delete|disable|bypass|revoke|login_failed|totp_failed)$/.test(action)) return 'danger'
  if (/^(user|org|setup|runner|protection)\./.test(action)) return 'warning'
  return 'neutral'
}

/**
 * The server version for display. Development builds report "dev" and an
 * "unknown" commit; show those as one plain phrase.
 */
export function versionLabel(version?: string | null, commit?: string | null): string {
  const v = (version ?? '').trim()
  const c = (commit ?? '').trim()
  const devBuild = !v || v === 'dev' || v === 'unknown'
  const base = devBuild ? 'development build' : v.startsWith('v') ? v : `v${v}`
  return c && c !== 'unknown' ? `${base} (${c.slice(0, 7)})` : base
}
