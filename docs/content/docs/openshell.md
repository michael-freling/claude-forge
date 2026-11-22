---
title: OpenShell Sandboxes
weight: 6
---

`claude-forge openshell` runs Claude Code in an
[OpenShell](https://github.com/NVIDIA/OpenShell) sandbox instead of a Docker
container. It is a separate runtime with a different model from
`claude-forge start`:

- **Not tied to a directory or repository.** You can run it from anywhere.
  Each sandbox has its own persistent workspace (`~/work`). Repositories you
  name are cloned into it when the sandbox is created, and the agent clones
  any others it needs, several side by side.
  Nothing on your machine is mounted, so the agent's work reaches you through
  what it pushes.
- **No gateway or git proxy.** Network access comes from a generated OpenShell
  policy: an allowlist of Anthropic, GitHub, PyPI, npm, AWS, GCP, Go modules
  and a few documentation sites. Anything else is blocked.
- **Credentials never enter the sandbox.** The Claude and GitHub credentials
  are held by OpenShell *providers*. The sandbox only sees placeholders, which
  OpenShell replaces with the real values on requests to Anthropic and GitHub.
  You never log in inside a sandbox.

## Prerequisites

- The `openshell` CLI and a running gateway (`openshell status`)
- The same Claude login `claude-forge start` uses: `ANTHROPIC_API_KEY`,
  `CLAUDE_CODE_OAUTH_TOKEN`, or your `claude` login in `~/.claude`. Nothing
  extra to set up.
- Optionally, a GitHub token: `GITHUB_TOKEN`, `GH_TOKEN`, or `gh auth token`.
  Without one, only public repositories can be cloned.

## Quick start

```bash
# Create a sandbox with two repositories cloned and start Claude Code in it
claude-forge openshell start api-work --repo octo/api --repo octo/web -p "..."

# Exit Claude Code; the sandbox keeps running. Later:
claude-forge openshell connect api-work   # continues the last conversation
claude-forge openshell stop api-work      # stop; the workspace is kept
claude-forge openshell connect api-work   # starts it again and continues
claude-forge openshell delete api-work    # removes the sandbox and its workspace
```

## Commands

| Command | What it does |
| --- | --- |
| `openshell start <name>` | Creates a sandbox and starts Claude Code in `~/work`. Flags: `--repo owner/name` (repeatable), `-p/--prompt`, `--agent`, `--no-skip-permissions` |
| `openshell connect <name>` | Runs Claude Code again with `--continue`, starting the sandbox first if it is stopped. Flags: `--agent`, `--no-skip-permissions` |
| `openshell list` | Lists the sandboxes claude-forge created (labelled `claude-forge=true`) |
| `openshell stop <name>` | Stops a sandbox, keeping its workspace |
| `openshell delete <name>` | Deletes a sandbox **and its workspace**. Push any work first |
| `openshell auth` | Re-syncs the Claude and GitHub credentials to OpenShell; running sandboxes pick them up within seconds |
| `openshell policy` | Prints the policy new sandboxes get |

## Credentials

`start` and `connect` copy your current Claude credential into OpenShell each
time, so a sandbox always starts with a valid token. With a `claude` login,
that is the short-lived access token from `~/.claude/.credentials.json`. The
refresh token never leaves your machine, because a sandbox that refreshed it
would sign out your host's Claude Code. If an access token expires while you
are attached (after roughly eight hours), run `claude` on the host to refresh
the login, then reconnect or run `claude-forge openshell auth`. Running
sandboxes pick up the new token within seconds.

The GitHub token is stored the first time a sandbox needs it. Run
`claude-forge openshell auth` after rotating it.

## What happens at creation

Before Claude Code starts in a new sandbox, `start`:

- **Clones repositories.** `openshell.repositories` and every `--repo` are
  cloned over HTTPS into `~/work/<name>`. When two repositories share a name,
  the later one goes to `~/work/<owner>-<name>`. A directory that already
  exists is skipped.
- **Installs Claude Code plugins.** Unless `openshell.plugins` is set, these
  are the plugins enabled on your machine (`enabledPlugins` in
  `~/.claude/settings.json`). Each plugin's marketplace is added first, from
  the source in your `~/.claude/plugins/known_marketplaces.json`. A
  marketplace you added from a local directory is added from that
  directory's GitHub `origin` remote instead.

A repository or plugin that fails (a typo, no access, an unknown marketplace)
prints a warning and the session starts anyway. `connect` does neither again:
the sandbox keeps what it cloned and installed, and the plugins stay enabled.

## What is copied into a sandbox

Every `start` and `connect` refreshes `~/.claude` in the sandbox from your
machine:

- your `~/.claude/CLAUDE.md`, after generated instructions about the sandbox
- `~/.claude/rules`, `agents`, `commands` and `skills`, with symlinks resolved
- `~/.config/claude-forge/settings.json`, minus the settings that only work in
  the Docker runtime: MCP servers, plugins and OpenTelemetry exporters. The
  plugins and marketplaces the sandbox installed itself are kept.

`start` also seeds `~/.claude.json` (onboarding done, your theme, the workspace
trusted) and `~/.gitconfig` (your global git `user.name`/`user.email`, and a
credential helper for GitHub over HTTPS). After that the sandbox owns them, so
`connect` leaves them alone.

The git identity comes from `git config --get` in the directory you run
claude-forge from. Set it globally (`git config --global user.name ...`) if
you want commits made in sandboxes to carry it.

## Configuration

```yaml
# ~/.config/claude-forge/config.yaml
openshell:
  gateway: ""          # OpenShell gateway name (default: the CLI's active gateway)
  image: ""            # sandbox image (default: images.agent)
  # Cloned into ~/work when a sandbox is created, before any --repo.
  repositories:
    - octo/api
  # Claude Code plugins installed when a sandbox is created.
  # Leave the key out to install the plugins enabled on your machine;
  # set it to [] to install none.
  plugins:
    - michael-config@michael-freling
  github:
    # Limit git push and GitHub REST API writes to these repositories.
    # Empty (the default) allows whatever the GitHub token allows.
    push_repos:
      - octo/api
      - octo/web
```

With `push_repos` set, the agent can still read and clone any repository the
token can access, but OpenShell blocks the following:

- `git push` to any other repository
- REST API writes (`POST`/`PUT`/`PATCH`/`DELETE`) outside `/repos/<listed repo>`
- every GraphQL mutation, because a mutation cannot be scoped to a repository
  by its URL

The setting takes effect for sandboxes created afterwards. Running sandboxes
keep the rules they started with.

## Limitations

- No Docker inside the sandbox, and no MCP servers from the Docker runtime.
- No `gh` CLI in the image. The agent calls the GitHub API with `curl` and the
  `$GITHUB_TOKEN` placeholder.
- git over SSH has no key. Use HTTPS remotes.
- Hosts outside the allowlist are unreachable. Check
  `claude-forge openshell policy` for the full list.
