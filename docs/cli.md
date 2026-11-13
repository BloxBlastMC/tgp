# The `tgp` CLI

`tgp` works with a tgp server from a terminal, scripts or AI agents. Every
command that prints data can print JSON, errors have stable codes, and exit
codes say what kind of failure happened.

## Logging in

```sh
tgp auth login --host git.example.com            # username, password, 2FA prompts
echo "$TOKEN" | tgp auth login --host git.example.com --with-token
tgp auth login --host http://localhost:3000 -u alice --password-stdin < pw.txt
tgp auth status                                    # checks each stored token works
tgp auth logout                                    # also revokes the token login created
```

A password login creates a personal access token called `tgp CLI on <hostname>`
with the scopes `read` and `write`, plus `admin` for admins. Use `--scopes` to
change them. If your account still has a temporary password, an interactive
login asks you to choose a new one. If 2FA is on, you're asked for a code, or
you can pass `--otp` with a code or a recovery code.

Where the token is kept:
- In the OS keyring (Secret Service on Linux, Keychain on macOS, Credential
  Manager on Windows) when one is available.
- Otherwise in `hosts.toml` in the config directory, with permissions 0600.
  That happens with `--insecure-storage`, with `TGP_NO_KEYRING=1`, or when no
  keyring answers within 3 seconds, which is normal on headless servers.

The config directory is `$TGP_CONFIG_DIR`, or `~/.config/tgp` on Linux.

**Scripts and agents** can skip the stored login: set `TGP_HOST` and
`TGP_TOKEN` and every command uses them.

## git over HTTPS

```sh
tgp auth setup-git            # git asks tgp for credentials for your servers
tgp repo clone game-server    # clone; this clone uses tgp for credentials
```

`tgp repo clone` stores `tgp auth git-credential` as the credential helper in
the new clone's `.git/config`, so later fetches and pushes from it just work.
`tgp auth setup-git` writes the same helper to your global git config for each
server you're logged in to (or only `--host`), so plain `git clone` works too.
With `TGP_TOKEN`, the token is passed to that one `git clone` and isn't
written anywhere.

## Which server and repository

**Server**, first match wins:
1. `--host`
2. `TGP_HOST`
3. the git remote of the current directory, if it points at a server you're logged in to
4. your default login (the first one you created)

**Repository**:
1. the `NAME` argument or `-R`/`--repo`, given as `name`, `host/name` or a clone URL
2. otherwise the current directory's git remote: `origin` first, then
   `upstream`, then any remote that points at a known server

## Commands

| Command | |
| --- | --- |
| `tgp auth login\|logout\|status\|token\|setup-git` | Logins and git credentials |
| `tgp repo list [-q text] [-L n] [--archived\|--no-archived]` | Repositories you can access |
| `tgp repo view [NAME]` | One repository |
| `tgp repo create NAME [-d text] [--default-branch b] [--init] [--clone]` | Create (admins) |
| `tgp repo clone NAME [DIR] [-- git flags]` | Clone over HTTPS |
| `tgp repo edit [NAME] [-d text] [--default-branch b] [--rename n] [--archive\|--unarchive]` | Change settings |
| `tgp repo delete NAME [--yes]` | Delete permanently (admins) |
| `tgp pr list [-s open\|closed\|merged\|all] [-A user] [-B base] [-H head] [-q text]` | Pull requests in a repository |
| `tgp pr status` | Waiting for your review, and your own, across repositories |
| `tgp pr view [PR] [--comments]` | A pull request; `--json` includes reviews, every comment and merge blockers |
| `tgp pr create [--fill] [-t title] [-b body\|-F file] [-B base] [-H head] [-r reviewers] [-d]` | Open a pull request |
| `tgp pr checkout PR` | Check out its branch |
| `tgp pr diff [PR] [--name-only]` | Its changes |
| `tgp pr comment [PR] -b text [--file path:line [--side old]] [--reply-to id]` | Comment, inline comment or reply |
| `tgp pr comments [PR] [--unresolved]` | All comments, inline ones with path, line and diff hunk |
| `tgp pr review [PR] --approve\|--request-changes\|--comment [-b text] [--inline path:line=text]` | Review |
| `tgp pr merge [PR] [--merge\|--squash\|--rebase] [-d] [--sha s] [--admin] [--sync]` | Merge |
| `tgp pr close\|reopen\|ready [--undo]\|update-branch\|edit [PR]` | Change state, base, title or reviewers |
| `tgp protection list\|set PATTERN [flags]\|delete PATTERN` | Branch protection rules (changing them: admins) |
| `tgp admin user list\|view\|create\|edit\|reset-password\|reset-2fa\|access` | Users (admins) |
| `tgp admin audit [--actor u] [--action prefix] [-L n] [--before id]` | Audit log (admins) |
| `tgp api [METHOD] PATH [-f k=v] [-F k=v] [--input file] [-H 'K: v'] [-i]` | Raw API request |
| `tgp schema [COMMAND...]` | JSON Schema of a command's output |
| `tgp version` | Version info |

`PR` is a number (`12` or `#12`), a pull request URL, or a head branch name;
left out, it's the pull request for the current git branch.

Run `tgp <command> --help` for flags and examples. Later milestones add
`issue`, `run`, `release` and more.

### A typical agent loop

```sh
git push -u origin my-change
tgp pr create --fill --reviewer tom --json=number
tgp pr view --json --jq '{can_merge, blockers: [.blockers[].message]}'
tgp pr comments --unresolved --json --jq '.[] | "\(.id) \(.path):\(.line) \(.body)"'
tgp pr comment --reply-to 381 -b "Fixed in 3f2a9c1"
tgp pr merge --squash --delete-branch --sha "$(git rev-parse HEAD)"
```

## JSON output

- `--json` prints every field.
- `--json=name,clone_url` (or `--json name,clone_url`) prints only those
  fields, from an object or from each object in a list. An unknown field is a
  usage error, and the error lists the fields that exist.
- `--jq '<expr>'` filters the output with [jq](https://jqlang.org) syntax,
  built in. String results print raw, without quotes.
- `--help` lists a command's JSON fields.
- `tgp schema <command>` prints the full JSON Schema (draft 2020-12).
  `tgp schema --json` lists every command and its schema name.

JSON is compact when piped and indented in a terminal.

```sh
tgp repo list --json=name,pushed_at
tgp repo list --jq '.[] | select(.permission == "write") | .name'
tgp repo view --jq .clone_url
tgp schema repo view
```

## Errors and exit codes

Errors go to stderr. With `--json` or `--jq` an error is a single line of JSON:

```json
{"error":{"code":"not_found","message":"repository \"nope\" not found","status":404}}
```

The object can also have `hint` (what to do next) and `details`. `code` is
either an API error code, such as `not_found`, `forbidden`, `validation_failed`,
`conflict`, `unauthenticated`, `password_change_required`, `totp_required` or
`setup_required`, or one of the CLI's own:

| Code | Meaning |
| --- | --- |
| `usage` | Bad arguments or flags |
| `input_required` | A prompt was needed but prompts are off; the message names the flag to pass |
| `not_logged_in` | No login or token for the server |
| `repo_required` | No repository given and none found in the current directory |
| `network_error` | Couldn't reach the server |
| `not_a_tgp_server` | The URL isn't a tgp server |
| `git_failed` | A git command failed |
| `cancelled` | A confirmation was declined |

| Exit | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Other error |
| 2 | Usage error or missing input (`usage`, `input_required`, `repo_required`, `bad_request`) |
| 3 | Authentication (`not_logged_in`, `unauthenticated`, `password_change_required`, `totp_required`) |
| 4 | Forbidden |
| 5 | Not found |
| 6 | Conflict or validation failure (`conflict`, `validation_failed`, `setup_required`) |
| 7 | Network error, server busy or rate limited: retrying may help |

These codes and exit statuses are stable. New ones may be added, but an
existing one never changes meaning.

## Prompts

`tgp` prompts only when both stdin and stdout are terminals. `--no-input` or
`TGP_NO_INPUT=1` turns prompts off even in a terminal. With prompts off,
anything that would have asked fails with `input_required`, and the message
names the flag to pass instead. For example, `repo delete` needs `--yes`.
Nothing ever waits on input that isn't coming.

## `tgp api`

```sh
tgp api /repos                                   # GET /api/v1/repos
tgp api PATCH repos/game -f description="Paper plugins"
tgp api POST admin/users -f username=carol -f role=tester -F all_repos=true
tgp api POST /repos --input repo.json            # body from a file (- for stdin)
tgp api /api/v1/openapi.json --jq '.paths | keys' # the API spec (public)
```

- `-f` adds a string field and `-F` a typed one: numbers, `true`, `false` and
  `null` are converted, and `@file` reads the value from a file.
- Fields become a JSON body, or the query string for GET and DELETE.
- Error responses exit with the codes above.

## Environment

| Variable | |
| --- | --- |
| `TGP_HOST` | Server to use |
| `TGP_TOKEN` | Token to use instead of the stored login |
| `TGP_CONFIG_DIR` | Config directory |
| `TGP_NO_KEYRING=1` | Never use the OS keyring |
| `TGP_NO_INPUT=1` | Never prompt |
| `TGP_DEBUG=1` | Log each HTTP request to stderr, without tokens |
| `NO_COLOR` | Plain output |
