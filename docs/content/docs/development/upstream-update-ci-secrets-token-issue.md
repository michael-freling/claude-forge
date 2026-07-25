# Handoff: file an upstream issue on `michael-freling/claude-code-tools`

**Status:** drafted here, **not yet filed.** This claude-forge session couldn't
post it: the session's GitHub MCP is write-scoped to `michael-freling/claude-forge`
only, and this host has no `gh` CLI and no `GITHUB_TOKEN` / `~/.config/gh` to use
with `curl`. Pick this up from a session that can write to `claude-code-tools`
(e.g. a claude-forge session scoped to that repo, or anywhere you have `gh`
authenticated).

## The task

Create one issue on **`michael-freling/claude-code-tools`** with the title and
body below. Suggested labels: `bug`, `enhancement`.

## Why (root cause, already traced)

The user runs `update-ci-secrets` to set the `CLAUDE_CODE_OAUTH_TOKEN` GitHub
Actions secret for their Claude PR-review workflow, and it expires within ~an
hour. They expected it to push a `claude setup-token` (~1 year) token.

It doesn't. `update-ci-secrets` defaults to `--from-credentials`, which pushes
the **short-lived interactive subscription access token** (rotated ~hourly), not
a setup-token — so the secret dies within the hour. `setup-token` output is
printed to the terminal and never written to `.credentials.json`, so
`--from-credentials` can't pick it up.

## How to file it

Option A — `gh` (run where you're authenticated with repo admin on
claude-code-tools):

```bash
gh issue create \
  --repo michael-freling/claude-code-tools \
  --label bug,enhancement \
  --title "update-ci-secrets --from-credentials uploads the ~1h subscription access token, so CLAUDE_CODE_OAUTH_TOKEN expires within the hour" \
  --body-file docs/content/docs/development/upstream-update-ci-secrets-token-issue.md
```

(Or paste just the **Issue body** section below; the file also contains this
handoff preamble.)

Option B — prefilled web form: open
`https://github.com/michael-freling/claude-code-tools/issues/new` and paste the
title and body.

Option C — from a claude-forge session scoped to `claude-code-tools`, the GitHub
MCP `github_issue_create` / `github_api` tools will be able to write there.

---

## Issue title

```
update-ci-secrets --from-credentials uploads the ~1h subscription access token, so CLAUDE_CODE_OAUTH_TOKEN expires within the hour
```

## Issue body

### Summary

`update-ci-secrets` (default `--from-credentials`) uploads the **short-lived
interactive subscription access token** — the one Claude Code rotates roughly
hourly — as the `CLAUDE_CODE_OAUTH_TOKEN` GitHub Actions secret. As a result the
secret expires within ~1 hour and the Claude PR-review workflow starts failing
with an expired-token error until `update-ci-secrets` is run again. This is
surprising: one reasonably expects a long-lived `claude setup-token` (~1 year)
to be used for CI.

### Root cause

Tracing the default path:

1. `cmd/update-ci-secrets/main.go` — `--from-credentials` defaults to `true`.
   With no `--oauth-token`, it calls `UpdateFromCredentials(ctx, ~/.claude)`.
2. `internal/cisecrets/cisecrets.go` — `UpdateFromCredentials` calls
   `auth.Resolve(claudeDir)`, requires `AuthType == "oauth"`, then `gh secret
   set CLAUDE_CODE_OAUTH_TOKEN` (value on stdin).
3. `internal/auth/auth.go` — `Resolve` order: `ANTHROPIC_API_KEY` env →
   `CLAUDE_CODE_OAUTH_TOKEN` env → `~/.claude/.credentials.json`
   `claudeAiOauth.accessToken` (and it errors if `expiresAt` has already
   passed).

That `claudeAiOauth.accessToken` is the interactive subscription **access
token** (it ships with `refreshToken` + `expiresAt` and is refreshed ~hourly by
Claude Code). `claude setup-token` mints a *separate* ~1-year token but
**prints it to the terminal and never writes it to `.credentials.json`**, so
`--from-credentials` can never pick it up — it always grabs the hourly access
token.

So each run uploads a token that already has <1h of life left.

### Impact

- CI (`.github/workflows/claude-review.yml`) fails with an expired-token error
  within ~1 hour of every `update-ci-secrets` run.
- Especially painful for long-running/automated flows (e.g. an agent that pushes
  a branch and waits on CI): the review job fails purely because the secret went
  stale mid-flight, requiring a manual re-run of `update-ci-secrets`.

### Proposed fixes (ranked)

1. **Warn when uploading a short-lived token.** Have `UpdateFromCredentials`
   surface `expiresAt` and print a clear warning when the token expires soon
   (e.g. `< 24h`), e.g. *"uploaded token expires in 47m; for CI use a setup-token
   via --oauth-token"*. Makes the footgun visible immediately. (Requires
   `auth.Resolve` — or a sibling — to also return `expiresAt`.)
2. **Document the behavior** in the command help/README: `--from-credentials`
   uses the hourly subscription access token; for durable CI auth, pass a
   `claude setup-token` value via `--oauth-token`.
3. **(Enhancement) Add a refresh/watch mode** — e.g.
   `update-ci-secrets --watch --interval 30m` that periodically re-resolves and
   re-pushes the current access token, so even the hourly token never goes stale
   hands-free. This is the natural home for the automation (rather than pushing
   it into downstream tools).
4. **(Optional) First-class setup-token support** — e.g. accept a setup-token on
   stdin, or a `--setup-token` flag that runs `claude setup-token` and pushes the
   result.

### Workarounds available today

- Run `claude setup-token` once, then push it explicitly:
  `update-ci-secrets --oauth-token <token>` → secret lasts ~1 year.
- Or `export CLAUDE_CODE_OAUTH_TOKEN=<setup-token>` before running, since
  `auth.Resolve` checks that env var before the credentials file.

### References

- `cmd/update-ci-secrets/main.go` — `--from-credentials` default `true`;
  `UpdateFromCredentials` on the default path.
- `internal/cisecrets/cisecrets.go` — `UpdateFromCredentials` → `auth.Resolve`;
  `SecretName = "CLAUDE_CODE_OAUTH_TOKEN"`.
- `internal/auth/auth.go` — `Resolve` returns `claudeAiOauth.accessToken`.
