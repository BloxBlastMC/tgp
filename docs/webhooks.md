# Webhooks

A webhook POSTs JSON to a URL when something happens: a push, a pull request
merge, a new issue, a published release, a finished CI run. Use them to
notify chat, trigger deployments or feed other tools.

## Adding a webhook

- **Repository webhooks:** repository **Settings → Webhooks**, or
  `tgp webhook create URL -e push -e pull_request`. Needs write access.
- **Organisation webhooks** get events from every repository, including
  ones created later: **Admin → Webhooks**, or `tgp webhook create URL --org`.
  Admins only.

Each webhook has:

- **URL:** `http://` or `https://`. Credentials belong in the secret, not
  the URL.
- **Events:** a list (default: `push`), or `*` for everything.
  `tgp webhook events` lists them.
- **Secret** (optional): signs every delivery (see below). It's stored
  encrypted and never shown again.
- **Active:** inactive webhooks get nothing.
- **Skip TLS verification:** for receivers with self-signed certificates.

A new webhook gets a `ping` event right away, so you see at once whether the
receiver is reachable.

## Events

| Event | Actions | When |
| --- | --- | --- |
| `push` | — | Commits or tags pushed, including merges and tags the server makes. Branch and tag deletions are pushes with `deleted: true`. |
| `create`, `delete` | — | A branch or tag was created or deleted. |
| `pull_request` | `opened`, `edited`, `closed`, `reopened`, `synchronize`, `ready_for_review`, `converted_to_draft`, `labeled`, `unlabeled`, `assigned`, `unassigned`, `milestoned`, `demilestoned`, `review_requested`, `review_request_removed` | Pull request activity. A merge is `closed` with `pull_request.state` = `merged`. |
| `pull_request_review` | `submitted`, `dismissed` | Reviews. |
| `pull_request_review_comment` | `created`, `edited`, `deleted` | Inline comments on the diff. |
| `issues` | `opened`, `edited`, `closed`, `reopened`, `labeled`, `unlabeled`, `assigned`, `unassigned`, `milestoned`, `demilestoned` | Issue activity. |
| `issue_comment` | `created`, `edited`, `deleted` | Comments on issues and pull request conversations. |
| `release` | `created`, `published`, `released`, `prereleased`, `edited`, `deleted` | Releases. Publishing sends `published` plus `released` or `prereleased`; drafts send `created`. |
| `workflow_run` | `requested`, `completed` | CI runs (re-runs send `requested` again). |
| `ping` | — | When the webhook is created, and from **Send ping** / `tgp webhook ping`. |

## Payloads

Payloads follow GitHub's shapes where tgp has the same concepts, so many
existing receivers work unchanged. Every payload has:

```json
{
  "action": "opened",
  "repository": {"id": 3, "name": "game", "full_name": "bloxblast/game", "default_branch": "main",
                 "html_url": "https://git.example.com/game", "clone_url": "https://git.example.com/game.git", "private": true},
  "sender": {"id": 7, "login": "mia", "name": "Mia", "type": "User"},
  "organization": {"login": "bloxblast"}
}
```

Then, by event:

- **`push`:** `ref`, `before`, `after`, `created`, `deleted`, `forced`, `compare`,
  `commits` (up to 20, oldest first, with `id`, `message`, `author`,
  `timestamp`, `url`), `total_commits`, `head_commit` and `pusher`.
- **`pull_request` / `issues`:** `number` and `pull_request` or `issue`, the
  same object the API returns. Also `label`, `assignee`, `milestone`,
  `requested_reviewer` or `changes` for those actions; `before` / `after`
  for `synchronize`.
- **`pull_request_review`:** `review` and `pull_request`.
- **`issue_comment` / `pull_request_review_comment`:** `comment` (API
  object) and `issue` or `pull_request` (`number`, `title`, `state`,
  `html_url`).
- **`release`:** `release` (API object, with assets).
- **`workflow_run`:** `workflow_run` with `id`, `run_number`, `run_attempt`,
  `name`, `path`, `event`, `status`, `conclusion`, `head_branch`, `head_sha`,
  `ref` and `html_url`.

## Headers and signatures

| Header | Value |
| --- | --- |
| `X-Tgp-Event` (and `X-GitHub-Event`) | The event name. |
| `X-Tgp-Delivery` (and `X-GitHub-Delivery`) | A unique ID per delivery; redeliveries get a new one. |
| `X-Tgp-Hook-ID` (and `X-GitHub-Hook-ID`) | The webhook's ID. |
| `X-Tgp-Signature-256` (and `X-Hub-Signature-256`) | `sha256=` + hex HMAC-SHA256 of the raw body with the secret (only with a secret). |
| `User-Agent` | `tgp-hookshot/<version>`. |

Verify the signature over the exact bytes you received, with a
constant-time comparison:

```python
import hmac, hashlib
def valid(secret: bytes, body: bytes, header: str) -> bool:
    expected = "sha256=" + hmac.new(secret, body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, header)
```

## Delivery

- **Success** is a 2xx answer within the timeout (10 seconds by default).
  Redirects aren't followed.
- **Order.** A webhook's deliveries are sent one at a time, oldest first, so
  the receiver sees events in the order they happened. Retries can still
  arrive late.
- **Retries.** Network errors, timeouts, 5xx, 408 and 429 answers are retried
  after 15 s, 1 min, 5 min, 30 min and 2 h; then the delivery has failed.
  Other 4xx answers fail at once (the receiver refused the request).
- **The delivery log** shows every delivery for 30 days: status, attempts,
  the request (URL, headers, payload) and the response (headers and the
  first 64 KiB of the body). Find it on the webhook's page or with
  `tgp webhook deliveries ID` and `tgp webhook delivery ID DELIVERY`.
- **Redeliver** sends a delivery's payload again as a new delivery (UI, or
  `tgp webhook redeliver ID DELIVERY`).
- Deliveries are queued in the database, so they survive restarts. With
  several server nodes, each delivery is sent by one node.

## Network safety

Deliveries never go to link-local addresses (such as the cloud metadata
service at 169.254.169.254). The address is checked when connecting, after
DNS. Proxies aren't used, so the check sees the real destination.

Receivers on localhost and private networks (10/8, 172.16/12, 192.168/16,
100.64/10, which includes Tailscale addresses, and fc00::/7) are refused by
default too. Delivery responses are visible to whoever manages the webhook,
so allowing these would let maintainers read internal services from the
server's point of view. If a receiver must live on an internal network, set
`block_private_networks = false` in `[webhooks]`
(`TGP_WEBHOOKS_BLOCK_PRIVATE=false`).

**Upgrading:** before this default changed, the example config shipped
`block_private_networks = false`. If you copied it, that line still allows
private destinations; delete it to get the new default.

Creating, changing and deleting webhooks is recorded in the audit log.
