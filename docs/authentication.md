# Authentication and accounts

## First start

1. Start `tgp-server`. While setup is pending it prints a box with a one-time
   **setup token** to stderr. Restarting prints a new token (the old one stops
   working), so only someone with access to the server's output can claim it.
2. Open the web UI. Every page redirects to `/setup` until setup is done.
3. The wizard asks for the token, the first admin account, the organization
   (name, description, base URL, logo) and optionally more team members.
   Everything is created in one transaction when you press **Complete setup**.
4. Temporary passwords for the extra users are shown once on the final screen.

## Roles

| Role       | What they can do                                                                 |
| ---------- | -------------------------------------------------------------------------------- |
| Admin      | Everything: admin panel, creating repositories, managing users.                  |
| Maintainer | Read/write on their repositories: push, merge, manage branches, CI and labels.   |
| Tester     | Read, issues, comments, PR reviews and CI re-runs on their repositories.         |

Maintainers and testers have **all repositories** or **selected
repositories**. Admins always have all. (Picking specific repositories
arrives with repositories in milestone 3.)

Safeguards: admins can't change their own role or disable themselves, and no
change may leave the org without an active admin.

## New users and temporary passwords

An admin creates a user (Admin → Users → New user, or `POST /admin/users`).
tgp generates a temporary password like `kq7v-Ma3x-9TzR-bw4e`, valid for
**72 hours**, shown once. At first login the user must choose their own
password; until then every API call except session, logout and change-password
fails with `password_change_required`. They're then offered 2FA setup.

Admins can issue a new temporary password at any time (Reset password). This
signs the user out everywhere.

## Two-factor authentication

TOTP (RFC 6238, 6 digits, 30 seconds, SHA-1: works with every authenticator
app). Enrolling shows a QR code and the secret; it activates only after a
valid code is entered. Ten single-use **recovery codes** are shown once.
Each TOTP code works only once. Admins can turn off a user's 2FA if they lose
their device. Secrets are stored encrypted with the server master key.

## Sessions

Browser sessions use an HttpOnly `tgp_session` cookie (SameSite=Lax, Secure
when the base URL is https), valid 30 days and extended while in use. Changing
your password signs out your other sessions.

Unsafe requests from browsers must send the `tgp_csrf` cookie value in the
`X-CSRF-Token` header (double-submit CSRF protection).

## Personal access tokens

Created under Settings → Personal access tokens. Format `tgp_pat_…`, shown
once, stored as a SHA-256 hash. Send as `Authorization: Bearer <token>` (or
`token <token>`), or as the password for git over HTTPS.

| Scope   | Allows                                                     |
| ------- | ---------------------------------------------------------- |
| `read`  | GET requests, cloning                                      |
| `write` | Everything else a user can do (implies `read`)             |
| `admin` | Admin endpoints (admins only)                              |

A token can never create a token with scopes it doesn't have. Disabling a
user stops their tokens immediately.

## Login protection

Failed logins are limited per username (10 per 15 minutes) and per client IP
(50 per 15 minutes); over the limit the API returns `rate_limited` with
`retry_after_seconds`. Limits are per server process. Unknown usernames take
as long to reject as wrong passwords. Passwords are hashed with argon2id.

## Audit log

Admin → Audit log (`GET /admin/audit-log`) records setup, logins (successful
and failed), logouts, password changes, 2FA changes, token creation and
revocation, user management and org changes, with actor, target, IP and
details. Filter by action prefix (`auth.`, `user.`…) or actor.
