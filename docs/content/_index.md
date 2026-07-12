---
title: claude-forge
layout: hextra-home
---

<div class="hx:mt-6 hx:mb-6">
{{< hextra/hero-headline >}}
  Run Claude Code autonomously,&nbsp;<br class="hx:sm:block hx:hidden" />without risking your machine
{{< /hextra/hero-headline >}}
</div>

<div class="hx:mb-12">
{{< hextra/hero-subtitle >}}
  claude-forge runs Claude Code with full autonomy inside an isolated&nbsp;<br class="hx:sm:block hx:hidden" />Docker container — a gateway proxy guards your GitHub access.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx:mb-6">
{{< hextra/hero-button text="Get Started" link="docs/getting-started" >}}
</div>

<div class="hx:mt-6"></div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Isolated by default"
    subtitle="Claude Code runs with `--dangerously-skip-permissions` — but inside a container, where the worst it can touch is the project you mounted, never your host."
  >}}
  {{< hextra/feature-card
    title="GitHub write protection"
    subtitle="A gateway proxy mediates all git and GitHub API traffic: read any repository, but push and PR creation are restricted to the current project only."
  >}}
  {{< hextra/feature-card
    title="Named, resumable sessions"
    subtitle="Run parallel sessions across projects, list them, resume any by name or ID, and prune old ones when you're done."
  >}}
  {{< hextra/feature-card
    title="MCP servers built in"
    subtitle="A per-session GitHub MCP sidecar scoped to your repo, an optional Kubernetes MCP gated by RBAC you generate, and any custom MCP server you configure."
  >}}
  {{< hextra/feature-card
    title="Docker-in-Docker, opt-in"
    subtitle="Give sessions their own isolated Docker daemon for building and running containers — with no view of the host's containers."
  >}}
  {{< hextra/feature-card
    title="Feels like your machine"
    subtitle="Host git identity, Claude Code configuration, plugins, and Go/npm/pnpm/pip caches are carried into every session automatically."
  >}}
{{< /hextra/feature-grid >}}
