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
  claude-forge runs Claude Code with full autonomy inside an isolated&nbsp;<br class="hx:sm:block hx:hidden" />Docker container — it reaches only your project and what you allow.
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
    title="One command, whole environment"
    subtitle="`claude-forge start` wires up the session network, gateway, and MCP sidecars, and tears them down again — `list`, `resume`, and `prune` manage the container environments for you."
  >}}
  {{< hextra/feature-card
    title="Your tools keep working"
    subtitle="Sandboxing usually breaks an agent's tooling. Here GitHub operations work out of the box via a repo-scoped MCP server, Kubernetes is available behind RBAC you control, and your own MCP servers plug in via config."
  >}}
  {{< hextra/feature-card
    title="Docker-in-Docker, opt-in"
    subtitle="Give sessions their own isolated Docker daemon for building and running containers — with no view of the host's containers."
  >}}
  {{< hextra/feature-card
    title="Zero setup inside the sandbox"
    subtitle="No re-configuring a fresh container every run: your git identity, Claude Code login, plugins, and dependency caches are carried in automatically, so sessions behave like your own machine."
  >}}
{{< /hextra/feature-grid >}}
