---
title: Secure Sandbox Environments for AI Coding Agents
weight: 1
---

# Secure Sandbox Environments for AI Coding Agents

This document surveys existing technologies, products, and approaches for securing AI coding agents (Claude Code, OpenAI Codex, Cursor, GitHub Copilot, etc.) across four layers: container/VM isolation, command-level security policies, network egress control (preventing unauthorized external service interactions), and secret/credential protection.

**Design constraint**: The agent must run **fully autonomously without human approval prompts**. All protection must come from automated guardrails — sandboxing, command policy, network filtering, credential proxies — not from human-in-the-loop approval workflows.

## 1. Container and VM Isolation

### What Major AI Agents Use

| Agent | Sandbox Approach |
|---|---|
| **OpenAI Codex** | Firecracker micro-VMs. Network disabled by default; user can allowlist specific domains. |
| **GitHub Copilot Coding Agent** | Ephemeral GitHub Actions runners (Azure VMs). Fresh VM per task. |
| **Google Jules** | Google Cloud sandboxed VMs, isolated per-session. |
| **Amazon Q Developer** | AWS-managed environments with scoped IAM permissions. |
| **Claude Code** | No built-in sandbox. Runs directly on the host machine. Anthropic recommends Docker with restricted permissions for CI/automated use. |

### Purpose-Built Sandbox Products

| Product | Type | Pricing | How It Works |
|---|---|---|---|
| **[E2B](https://e2b.dev)** | Cloud micro-VMs for AI agents | Freemium — free 100 hrs/mo, Pro $45/mo, Enterprise custom | Firecracker-based. Per-session sandboxes with filesystem, network, and process isolation. Purpose-built for AI code execution. |
| **[Daytona](https://daytona.io)** | Dev environment manager | Free (Apache 2.0 self-hosted), cloud has paid plans | Standardized dev environments via Docker/devcontainers. Positioning for AI agent workloads. |
| **[Modal](https://modal.com)** | Serverless containers | Freemium — $30/mo free credits, pay-per-use after | Not agent-specific but widely used for AI workloads. Fast cold starts, GPU support. |
| **[Fly Machines](https://fly.io)** | Micro-VM platform | Freemium — free small allowance, pay-per-use after | Firecracker-based. Some AI agent platforms use these as execution backends. |

### Underlying Isolation Technologies

| Technology | Type | Pricing | Who Uses It |
|---|---|---|---|
| **[Firecracker](https://firecracker-microvm.github.io/)** (AWS) | Micro-VMs, sub-second boot | Free (Apache 2.0) | E2B, Fly.io, OpenAI Codex |
| **[gVisor](https://gvisor.dev)** (Google) | User-space kernel, syscall interception | Free (Apache 2.0) | GKE, cloud sandboxes |
| **[Kata Containers](https://katacontainers.io)** | Lightweight VMs via OCI | Free (Apache 2.0) | Enterprise Kubernetes |
| **WebAssembly (Wasm)** | Instruction-level sandbox | Free (various) | Emerging; limited POSIX support for agent workloads |

**Key takeaway**: Firecracker micro-VMs are the dominant pattern for production AI agent sandboxing. E2B is the most purpose-built open-source option. Docker containers are the most common DIY approach but provide weaker isolation boundaries than micro-VMs.

---

## 2. Destructive Command Blocking

Container isolation prevents escape to the host, but does not prevent an AI agent from running destructive commands *within* the sandbox (deleting repos, force-pushing, dropping databases, destroying cloud resources). A second layer is needed.

### Open Source Tools

#### Destructive Command Guard (DCG)

**Repository**: [github.com/Dicklesworthstone/destructive_command_guard](https://github.com/Dicklesworthstone/destructive_command_guard)
**Language**: Rust
**Stars**: 872+
**Status**: Most mature tool in this space

DCG uses a four-stage pipeline:
1. JSON parsing of the command
2. Path normalization
3. O(n) quick-reject substring search
4. Pattern matching (safe patterns first, then destructive)

Ships with 49+ rule packs covering:
- **git**: `git push --force`, `git reset --hard`, `git clean -fd`
- **filesystem**: `rm -rf /`, `rm -rf ~`, `chmod -R 777 /`
- **database**: `DROP DATABASE`, `DROP TABLE`, `TRUNCATE`
- **Kubernetes**: `kubectl delete namespace`, `kubectl delete node`
- **cloud (GCP)**: `gcloud container clusters delete`, `gcloud projects delete`
- **Docker**: `docker system prune`, `docker rm -f $(docker ps -aq)`
- **Terraform**: `terraform destroy`

Integrates with Claude Code, Gemini CLI, and GitHub Copilot CLI via PreToolUse hooks. Whitelist-first architecture with `fail_open = false` to block unparseable commands.

#### Other Tools

All free and open source:

| Tool | Description | Maturity |
|---|---|---|
| **[agent-guardrails](https://github.com/roboticforce/agent-guardrails)** | Shell-based, focuses on terraform/database/k8s/cloud commands | Very early (2 stars) |
| **[pi-guardrails](https://github.com/aliou/pi-guardrails)** | TypeScript hooks for Pi IDE. Prevents dangerous operations, protects env files. | 58 stars |
| **[anywhere-agents](https://github.com/yzhao062/anywhere-agents)** | Portable agent configuration with built-in destructive-command guard. One config across all agents. | 155 stars |

### Native Agent Permission Systems

| Agent | How It Works | Limitations |
|---|---|---|
| **Claude Code** | Three-tier model: Allow (auto-approve), Ask (prompt user — **not applicable for autonomous execution**), Deny (block, always wins). Configured in `.claude/settings.json`. Supports glob patterns for Bash commands. PreToolUse hooks allow external tools like DCG. | Deny rules can be bypassed via token-budget attacks ([reported by Adversa AI](https://adversa.ai/blog/claude-code-security-bypass-deny-rules-disabled/)). |
| **OpenAI Codex** | OS-enforced sandbox. Approval policies: untrusted, on-request, never, or granular. Supports deny-read glob policies. | Has a `danger-full-access` escape hatch — **violates both invariants, must not be used**. |
| **Cursor** | Curated safe tools by default. Auto-run mode can be disabled org-wide. | CVE-2026-22708: shell built-ins (`export`, `typeset`) bypass command allowlists entirely. |
| **GitHub Copilot** | Organization/enterprise policies via admin dashboard. MCP server allowlists. | No granular shell command policy. |

---

## 3. Network Egress Control and External Service Prevention

Even within a sandboxed container, an AI agent with network access can interact with external services in unintended ways — posting comments on GitHub PRs, sending Slack messages, creating issues, triggering CI pipelines, commenting on Jira tickets, or calling arbitrary APIs. This section covers tools and approaches for restricting what external services an agent can reach and how it can interact with them.

### Threat Vectors

1. **Unintended PR/issue comments**: Agent uses `gh pr comment`, `gh issue comment`, or the GitHub API to post on public repositories
2. **Messaging services**: Agent sends Slack messages via webhook URLs, emails via SMTP, or posts to Discord/Teams
3. **CI/CD triggers**: Agent triggers deployments via API calls to allowed domains (e.g., GitHub Actions workflow dispatch on `api.github.com`)
4. **Data posting to allowed APIs**: Even with a domain allowlist, an agent can POST arbitrary data to allowed domains (e.g., creating gists on `api.github.com`, uploading files to `googleapis.com`)
5. **MCP server abuse**: Agent uses connected MCP servers (Slack, Gmail, Google Calendar) to send messages or create events as the user

### Network-Level Egress Filtering Tools

| Tool | Type | Pricing | How It Works |
|---|---|---|---|
| **[Squid](http://www.squid-cache.org/)** | HTTP forward proxy | Free (GPL) | Domain allowlist via ACLs. Supports HTTPS via CONNECT tunneling. Cannot inspect encrypted payloads but can restrict which domains are reachable. |
| **[mitmproxy](https://mitmproxy.org/)** | HTTPS-intercepting proxy | Free (MIT) | Can inspect and filter HTTPS traffic by URL path, HTTP method, and request body. Enables fine-grained rules like "allow GET to api.github.com but block POST to /repos/*/comments". Requires CA certificate trust in the sandbox. |
| **[Cilium](https://cilium.io/)** / eBPF | Kernel-level network policy | Free (Apache 2.0), Isovalent Enterprise (paid, now Cisco) | Fine-grained per-pod network policies in Kubernetes. Can restrict egress by domain, port, and protocol. Works at L3/L4/L7. |
| **[Envoy Proxy](https://www.envoyproxy.io/)** | L7 proxy | Free (Apache 2.0) | Can enforce per-route policies including HTTP method restrictions, rate limiting, and request body inspection. Used as a sidecar in service mesh architectures. |
| **Docker `--network=none`** | Built-in Docker flag | Free | Completely disables networking. Simplest option when the agent needs no internet at all. |
| **E2B network controls** | Built-in to E2B platform | Included in E2B pricing | Configurable per-sandbox network access with domain-level restrictions. |

### HTTP Method-Level Filtering

Domain allowlists alone are insufficient — an agent allowed to reach `api.github.com` for `git clone` can also POST comments, create issues, or delete branches on the same domain. Finer-grained filtering is needed:

**mitmproxy with scripted rules** is the most practical open-source approach for HTTP method + path filtering:

```python
# Example mitmproxy addon: allow reads, block writes to GitHub API
from mitmproxy import http

BLOCKED_PATTERNS = [
    ("POST",   "/repos/*/issues/*/comments"),
    ("POST",   "/repos/*/pulls/*/comments"),
    ("POST",   "/repos/*/issues"),
    ("POST",   "/repos/*/dispatches"),
    ("DELETE", "/repos/*"),
    ("PUT",    "/repos/*/pulls/*/merge"),
    ("POST",   "/gists"),
]

def request(flow: http.HTTPFlow) -> None:
    for method, pattern in BLOCKED_PATTERNS:
        if flow.request.method == method and matches(flow.request.path, pattern):
            flow.response = http.Response.make(403, b"Blocked by sandbox policy")
```

**Envoy with route-level config** can achieve similar results declaratively via YAML route rules.

### MCP Server Restrictions

AI coding agents increasingly connect to external services via MCP (Model Context Protocol) servers — Slack, Gmail, Google Calendar, GitHub, etc. An agent with access to the Slack MCP server can send messages as the user.

| Approach | How It Works |
|---|---|
| **Claude Code MCP allowlists** | In `.claude/settings.json`, configure which MCP servers are available. Deny-list specific MCP tools (e.g., allow `slack_read_channel` but deny `slack_send_message`). |
| **Don't mount MCP servers in sandbox** | Simply don't configure MCP server connections in the sandbox environment. The agent can only use CLI tools. |
| **Read-only MCP wrappers** | Wrap MCP servers in a proxy that strips write operations, only exposing read/search tools. |

### How Major Agents Handle Egress

| Agent | Network Policy |
|---|---|
| **OpenAI Codex** | Network disabled by default. User explicitly allowlists domains. Even when enabled, only outbound HTTPS is permitted. No fine-grained method filtering. |
| **GitHub Copilot Coding Agent** | Runs in Actions runners with standard GitHub Actions network access. No additional egress restrictions beyond what the runner provides. |
| **Claude Code** | No network restrictions by default — runs on host. In Docker, relies on user-configured network policies. |
| **E2B** | Configurable per-sandbox. Can be fully isolated or allowlist-based. |

### Recommended Layered Approach

| Layer | Tool | What It Blocks |
|---|---|---|
| Domain allowlist | Squid proxy | All traffic to non-approved domains |
| HTTP method + path filtering | mitmproxy or Envoy | Write operations to approved domains (commenting, posting, deleting) |
| MCP server restriction | Claude Code settings / don't mount | Agent access to messaging, email, calendar services |
| Token scoping | Fine-grained GitHub PATs, read-only GCP service accounts | Blast radius even if write requests get through |
| CLI tool restriction | Claude Code Deny rules, DCG | `gh pr comment`, `gh issue create`, `curl -X POST`, `slack` CLI |

---

## 4. Secret and Credential Isolation

The agent must run authenticated CLI tools (kubectl, gcloud, git push, gh), but **must never have access to the underlying credentials** (Invariant 1). This is a hard architectural constraint — the agent cannot read any secrets, period. This section covers how to make CLI tools work without giving the agent credential access.

### The Core Problem

```
The agent needs to run:    kubectl get pods
Which requires:            ~/.kube/config (contains cluster CA, auth token)
The agent CANNOT:          cat ~/.kube/config → credential files must not exist in the sandbox
```

This is why credential files must never be mounted in the sandbox. If they were, the agent could encode them (`base64`), embed them in a git commit, POST them to an allowed API, or include them in a subsequent prompt. Network filtering and command blocking don't help once the agent already has the secret.

### Approach 1: Credential Proxies — Never Give the Agent the Secret

Instead of mounting credential files into the sandbox, run a proxy *outside* the sandbox that injects authentication on behalf of the agent. The agent makes unauthenticated requests; the proxy adds credentials before forwarding.

| Tool | What It Does | Pricing |
|---|---|---|
| **[Infisical Agent Vault](https://github.com/Infisical/agent-vault)** | HTTP credential proxy purpose-built for AI agents. Agent routes requests through localhost proxy; proxy injects vault-stored credentials. Agent never possesses them. | Free (open source) |
| **`kubectl proxy`** | Binds to `127.0.0.1:8001`, forwards requests to K8s API using kubeconfig credentials. Agent talks to localhost; never reads kubeconfig. Scope down with RBAC. | Free (built into kubectl) |
| **SSH agent forwarding** | Agent inherits `$SSH_AUTH_SOCK` (Unix socket). Can request signing operations but cannot extract the private key. Socket only works while session is alive. | Free (built into OpenSSH) |
| **Git credential helpers** | Git's credential helper protocol serves tokens on demand via stdin/stdout without storing them in a file the agent can read. | Free (built into git) |
| **[abox](https://github.com/X-McKay/abox)** | MicroVM sandbox with policy-enforcing credential proxy (Rust). Credentials injected at the proxy layer, not inside the VM. | Free (open source) |
| **[botbox](https://github.com/reoring/botbox)** | K8s sidecar with deny-by-default egress and credential injection. | Free (open source) |
| **GCP metadata server emulator** | For gcloud inside containers, emulate the GCE metadata server to serve short-lived access tokens via GCP Workload Identity Federation. The agent calls `gcloud` normally; the metadata server provides tokens without a service account key file. Alternatively, inject a short-lived access token as an env var and configure `gcloud auth activate-service-account` with it in the entrypoint (before the agent starts). | Free (GCP feature) |

**How it works with kubectl proxy:**
```
┌─────────────────────────┐    ┌──────────────────────────────────┐
│  Sandbox (no kubeconfig) │    │  Host / Sidecar                  │
│                          │    │                                  │
│  Agent runs:             │    │  kubectl proxy --port=8001       │
│  curl localhost:8001/    │───▶│  (has kubeconfig, adds auth)     │
│    api/v1/pods           │    │         │                        │
│                          │    │         ▼                        │
│  Agent CANNOT:           │    │  K8s API server (production)     │
│  cat ~/.kube/config      │    │                                  │
│  (file doesn't exist)    │    │  RBAC limits what agent can do   │
└─────────────────────────┘    └──────────────────────────────────┘
```

### Approach 2: Filesystem-Level Secret Hiding (Weaker Alternative)

Make credential files usable by CLI tools but not readable by the agent process. **Note**: This approach relies on correct filesystem permission configuration rather than the absence of secrets. A misconfigured ACL or privilege escalation exposes the credential. The architecture in Section 7 prefers not mounting credential files at all (Approach 1). Use this only when credential proxies are not feasible.

| Tool | What It Does | Pricing |
|---|---|---|
| **[SecretFS](https://github.com/obormot/secretfs)** | FUSE filesystem with per-process ACLs. Credential file exists but returns `EACCES` to unauthorized processes. Can restrict by binary name, user, group, and time window. | Free (open source) |
| **Separate user + `sudo`** | Run CLI tools under a different user that owns the credential files. The agent's user cannot read them. Tools are invoked via `sudo -u creduser kubectl ...` | Free (OS-level) |
| **`/proc` hardening** | Mount procfs with `hidepid=2` so the agent cannot read `/proc/PID/environ` of other processes (which would reveal injected env vars). | Free (Linux kernel) |

### Approach 3: Short-Lived Credential Injection

Instead of mounting long-lived credentials (service account keys, SSH keys, PATs), generate short-lived tokens that auto-expire.

| Tool | What It Does | Pricing |
|---|---|---|
| **[ghtkn](https://github.com/suzuki-shunsuke/ghtkn)** | Generates GitHub App installation tokens (1-hour TTL, per-repo scope). No long-lived PAT needed. | Free (open source) |
| **GCP Workload Identity Federation** | Exchange an OIDC token for a 1-hour GCP access token via STS. No service account key file ever exists. | Free (GCP feature) |
| **HashiCorp Vault** | Issues short-lived dynamic credentials for databases, cloud providers, SSH certificates. Auto-expires. | Free (open source), Enterprise paid |
| **AWS STS `AssumeRole`** | Generate temporary credentials (15 min to 12 hours) with scoped permissions. | Free (AWS feature) |

**Why short-lived credentials help**: Even if the agent reads and exfiltrates a token, it expires in minutes/hours. Combined with network egress filtering, the window for exfiltration is extremely narrow.

### Approach 4: Secret Cloaking — Defense-in-Depth

Under Invariant 1, credentials should not be present in the sandbox in the first place (see Approach 1). However, short-lived tokens may appear in command stdout (e.g., `gcloud auth print-access-token`). Cloaking addresses this residual risk by scrubbing secret values before they reach the LLM's context.

| Tool | What It Does | Pricing |
|---|---|---|
| **[immunity-agent](https://github.com/PrismorSec/immunity-agent)** | Auto-cloaks secrets in prompts (replaces with hashed placeholders). Substitutes real values only at execution time. Scrubs stdout before model sees output. Works across Claude Code, Codex, Cursor, Windsurf. | Free (open source) |
| **[agent-env](https://agent-env.com/)** | Injects secrets via `exec` (process replacement) so they never touch shell history, `.env` files, or disk. Supports SOPS encryption. | Free (open source) |

### Approach 5: Exfiltration Prevention (Last Line of Defense)

If the agent does read a secret, prevent it from sending it anywhere.

| Layer | Tool | What It Prevents | Pricing |
|---|---|---|---|
| Network egress filtering | Squid proxy with domain allowlist | Exfiltration to arbitrary servers | Free |
| Git-level scanning | gitleaks, truffleHog pre-commit hooks | Secrets committed to repos | Free |
| Output scanning | Nightfall AI | ML-based secret detection in agent output | Freemium, paid from ~$10k+/yr |
| Credential scoping | Fine-grained GitHub PATs, GCP service accounts | Blast radius if exfiltration succeeds | Free |

### Established Credential Managers with Agent Patterns

- **Bitwarden Secrets Manager**: Agent-specific integration for injecting secrets without writing to disk. Freemium — free for personal use, Teams $4/user/mo, Enterprise $6/user/mo.
- **1Password `op-env`**: Wraps agent processes to inject secrets from 1Password vaults at runtime. Paid — Individual $2.99/mo, Teams $19.95/mo (up to 10 users), Business $7.99/user/mo.

### What OpenAI Codex Does

Codex uses a **two-phase runtime model**:
1. **Setup phase** (has network, can install dependencies, accesses secrets) — runs before the agent
2. **Agent phase** (network-isolated, secrets removed) — the AI runs here

Secrets are removed before the agent starts. Exfiltration is blocked because the agent has neither the secrets nor network egress. When sandbox restrictions are weakened (`--sandbox danger-full-access`), OpenAI explicitly warns that exfiltration becomes possible. **This mode violates both invariants and must not be used.**

### Recommended Approach

All layers are required for defense-in-depth. The numbering indicates protection strength, not an adoption order — all must be implemented to satisfy Invariant 1.

| Layer | Approach | What It Does |
|---|---|---|
| **Credential proxies** (primary) | kubectl proxy, SSH agent forwarding, git credential helpers, Infisical | Agent never has the secret — enforces Invariant 1 at the architectural boundary |
| **Short-lived tokens** (primary) | ghtkn, GCP Workload Identity, Vault | Where proxies aren't feasible, tokens expire in minutes/hours |
| **Secret cloaking** (defense-in-depth) | immunity-agent | Scrubs residual secrets (e.g., tokens in stdout) before they reach the LLM |
| **Egress filtering** (defense-in-depth) | Squid + mitmproxy | Even if a secret is read, limits where it can be sent |
| **Git scanning** (defense-in-depth) | gitleaks pre-commit | Catches secrets before they're pushed to repos |

---

## 5. Comprehensive Policy Engines

These tools attempt to provide a unified governance layer across all three concerns.

### Microsoft Agent Governance Toolkit

**Repository**: [github.com/microsoft/agent-governance-toolkit](https://github.com/microsoft/agent-governance-toolkit)
**License**: MIT
**Released**: April 2026

The most ambitious open-source entry in this space. Seven packages spanning Python, TypeScript, Rust, Go, and .NET.

Key components:
- **Agent OS**: Stateless policy engine intercepting every agent action before execution at sub-millisecond latency (p99 < 0.1ms)
- **Execution rings**: Modeled on CPU privilege levels for graduated permission escalation
- **Saga orchestration**: For multi-step agent workflows with rollback
- **Kill switches**: Emergency halt of agent execution
- Covers all 10 OWASP Agentic AI Top 10 risks
- Integrates with LangChain, CrewAI, and Microsoft Agent Framework

### Oktsec

**Repository**: [github.com/oktsec/oktsec](https://github.com/oktsec/oktsec)

Single-binary security layer for agent-to-agent communication:
- Ed25519 message signing for authenticity
- 175 detection rules aligned with OWASP Top 10 for Agentic Applications
- SQLite audit logging — every tool call produces an immutable audit entry

### Enterprise/Commercial Solutions

| Product | Focus | Pricing | Notes |
|---|---|---|---|
| **[Pillar Security](https://pillar.security)** | Agentic AI governance | Paid — enterprise pricing, contact sales | Gartner 2026 Representative Vendor. Maps agent/tool/permission graphs, runs adversarial tests, enforces data privacy at runtime. |
| **[Lakera](https://lakera.ai)** | Prompt injection defense + data protection | Freemium — free developer tier (limited calls), paid scales with usage, enterprise custom | Real-time guard via single API call. |
| **[Galileo Agent Control](https://galileo.ai)** | Control plane for agent governance | Freemium — free tier available, paid for production scale | Partners: AWS, CrewAI, Glean. Centralized policy with runtime updates. |
| **[Nightfall AI](https://nightfall.ai)** | DLP for AI agent output | Freemium — free tier for limited scanning, paid from ~$10k+/yr | AI-native classifiers (95% accuracy) for detecting secrets in output across SaaS, endpoints, and AI apps. |

---

## 6. Scalability: Boundaries Over Enumerations

Sections 2–5 describe tools that rely on enumerating what to block: lists of dangerous commands, domain allowlists, per-MCP-tool deny rules, URL path patterns. This approach has a fundamental scalability problem — every new tool, API, or MCP server requires manual configuration updates. The ecosystem is converging on a different model: **define a boundary, not an enumeration**.

### Pattern 1: Default-Deny with Opt-In Capabilities

Start with nothing allowed. The agent has no network, no filesystem access beyond its workspace, no credentials. Capabilities are granted explicitly and narrowly.

**Who does this**: OpenAI Codex runs agents in OS-enforced sandboxes with network disabled and filesystem writes restricted to the workspace by default. Users opt into network access per-domain. Web search uses a pre-indexed cache rather than live fetches.

**Trade-off**: High friction for workflows requiring `npm install`, API calls, or external tools. Every project needs explicit opt-in configuration. But the security surface stays small by default, and you never have to maintain a blocklist.

**Analogy**: iOS app permissions. Apps start with nothing. Each capability (camera, location, contacts) requires an explicit user grant. You don't enumerate what apps can't do.

### Pattern 2: Capability-Based Sandboxing (WASI Model)

Every tool call runs inside a sandbox that only has the capabilities explicitly granted to it. The tool declares what it needs; the runtime grants exactly that and nothing more.

**Who does this**: Microsoft's [Wassette](https://opensource.microsoft.com/blog/2025/08/06/introducing-wassette-webassembly-based-tools-for-ai-agents/) wraps every AI agent tool call in a WebAssembly (WASI) module. The module's accessible operations — file paths, network endpoints, environment variables — are described entirely by its capability grants. No capability grant means the tool physically cannot access the resource, enforced at the sandbox boundary.

**Why it scales**: You don't maintain a list of blocked actions. You maintain a small set of granted capabilities. The boundary enforces everything else. Adding a new MCP server or CLI tool doesn't require updating a blocklist — it just starts with zero capabilities.

**Analogy**: Unix file descriptors. A process can only access file descriptors passed to it. It cannot open arbitrary files — the capability must be explicitly granted by the parent.

### Pattern 3: Automated Risk Classification (No Human Approval)

Instead of pre-configuring every possible action, classify actions by risk at runtime. High-risk actions are **automatically blocked** (not paused for human approval). Novel or unrecognized actions default to deny.

**Risk dimensions for automated classification**:
- Read vs. write
- Internal (workspace) vs. external (network/API)
- Data sensitivity (credentials, PII vs. source code)
- Reversibility (git commit vs. git push --force)
- Audience (local file vs. public PR comment)

**Who does this**:
- **Microsoft Agent Governance Toolkit** — stateless policy engine that auto-blocks actions based on declarative rules at sub-millisecond latency, no human in the loop
- **DCG** — automatically blocks destructive commands without prompting, using pattern matching and quick-reject

**Why it scales**: No need to pre-enumerate every dangerous URL or command. The system classifies actions dynamically and blocks automatically. New tools and APIs are handled by the classification engine, not by manual config updates. No human is required to be watching.

**Note**: Human-in-the-loop approval patterns (Claude Code Ask mode, LangGraph interrupt(), Codex suggest mode) exist but are **not applicable** when the agent must run fully autonomously. All protection must come from automated guardrails.

### Pattern 4: Narrow Roles with Just-in-Time Credentials

Define agent roles like `code_reviewer`, `ci_runner`, or `deploy_agent` that bundle specific, time-limited permissions. Agents request credentials at runtime (just-in-time); credentials are discarded after the task completes.

**Who does this**:
- **[Oso](https://www.osohq.com/learn/ai-agent-permissions-delegated-access)** — delegated authorization for AI agents with policy-as-code. Library is free (Apache 2.0); Oso Cloud freemium from ~$149/mo.
- **[WorkOS](https://workos.com/blog/ai-agent-access-control)** — agent access control with fine-grained authorization. Freemium — free up to 1M MAUs (AuthKit), SSO/SCIM from ~$125/mo per connection.
- **[Auth0](https://auth0.com/blog/access-control-in-the-era-of-ai-agents/)** — identity-based agent permissions. Freemium — free up to 25k MAUs, paid from $35/mo (Essentials), enterprise custom.

**Why it scales**: Permissions are defined at the role level, not the action level. A `code_reviewer` role can read repos and post review comments but cannot merge, deploy, or access production secrets. Adding a new tool doesn't require updating the role definition unless the tool needs a capability the role doesn't have.

**Anti-pattern**: Long-lived service account tokens. An agent with a permanent GitHub PAT retains access to everything the token allows, indefinitely, regardless of what task it's performing.

### Comparison of Approaches

### Comparison of Approaches

| Approach | Scales to new tools? | Maintenance burden | Autonomous? | Security strength |
|---|---|---|---|---|
| Blocklist enumeration (DCG, allowlists) | No — each new tool needs rules | High | Yes | Medium — bypasses via unknown tools |
| Default-deny + opt-in (Codex) | Yes — new tools start blocked | Low | Yes | High |
| Capability-based sandbox (WASI) | Yes — boundary enforces | Low | Yes | Very high |
| Automated risk classification | Yes — classification handles novel actions | Low | Yes | High |
| Role-based + JIT credentials | Yes — roles abstract over actions | Medium — roles need design | Yes | High |
| Human-in-the-loop (Ask mode) | Yes | Low | **No — requires human** | High |

### Practical Recommendation

For fully autonomous agent execution, the most practical architecture combines these patterns:

1. **Start with default-deny**: network off, no MCP servers, workspace-only filesystem, no credential files. This is the boundary — everything is blocked by default.
2. **Grant capabilities per-project** via a config file: "this project needs GitHub API (read + push), npm registry, and Google Cloud APIs." This is the small opt-in surface.
3. **Use credential proxies** instead of mounting credential files: kubectl proxy, SSH agent forwarding, git credential helpers. The agent never possesses the secret.
4. **Issue JIT credentials** scoped to the task: a GitHub App installation token that expires in 1 hour, scoped to a single repo, with no `admin` permissions. **Residual risk**: JIT tokens may appear in command stdout (e.g., from a credential helper). Mitigate with secret cloaking (immunity-agent) + egress filtering + short expiry.
5. **Automatically block destructive commands** via DCG or similar policy engine — no human confirmation, just deny.

This eliminates the need for human approval while maintaining strong security guarantees. The agent runs at full speed; the guardrails are infrastructure, not prompts.

---

## 7. Recommendations

The sandbox runs locally but connects to production services (GitHub, GCP, Kubernetes clusters, etc.). Security must protect real production environments, not just the local machine.

### Recommended Stack (All Free / Open Source)

All components are required to satisfy both invariants. This is not an incremental adoption list.

| Layer | Tool | Enforces | Cost |
|---|---|---|---|
| Process isolation | Docker container with `no-new-privileges`, `cap_drop: ALL` | Both | Free |
| Credential proxies | **kubectl proxy**, **SSH agent forwarding**, **git credential helpers**, **Infisical Agent Vault** — run on host, outside sandbox. No credential files mounted in sandbox. | Invariant 1 | Free |
| Short-lived tokens | **ghtkn** (1-hr GitHub App tokens), **GCP Workload Identity Federation** (1-hr access tokens) | Invariant 1 | Free |
| Command policy | **DCG** as PreToolUse hook — automatic, no human prompts | Invariant 2 | Free |
| Domain-level egress | **Squid proxy** with domain allowlist | Invariant 1 | Free |
| Method-level egress | **mitmproxy** with scripted rules (block POST to comment/issue/gist endpoints) | Invariant 2 | Free |
| MCP restriction | Don't mount MCP servers (Slack, Gmail, etc.) in sandbox | Both | Free |
| Secret cloaking (defense-in-depth) | **immunity-agent** — scrubs short-lived tokens from command stdout before they reach the LLM | Invariant 1 | Free |
| Git scanning (defense-in-depth) | **gitleaks** pre-commit hook — catches secrets before they're pushed | Invariant 1 | Free |

All components are free and open source.

### Stronger Isolation (When Needed)

If Docker container isolation is insufficient (e.g., running untrusted agent code), consider upgrading the isolation layer:

| Option | Cost | What It Adds |
|---|---|---|
| **Firecracker** micro-VMs | Free (Apache 2.0) | Hardware-level isolation, sub-second boot, used by Codex |
| **E2B** | Free up to 100 hrs/mo, Pro $45/mo | Managed Firecracker sandboxes purpose-built for AI agents |
| **gVisor** | Free (Apache 2.0) | Syscall-level interception without full VM overhead |

### Stronger Policy Enforcement (When Needed)

If DCG's static rule packs are insufficient (e.g., needing runtime classification or multi-agent governance):

| Option | Cost | What It Adds |
|---|---|---|
| **Microsoft Agent Governance Toolkit** | Free (MIT) | Stateless policy engine, execution rings, kill switches, OWASP coverage |
| **Galileo Agent Control** | Free tier, paid for scale | Centralized policy control plane |
| **Pillar Security** | Enterprise pricing (contact sales) | Full agent governance with adversarial testing |

### Secret Detection in Outputs (When Needed)

| Option | Cost | What It Adds |
|---|---|---|
| **gitleaks** / **truffleHog** pre-commit hooks | Free | Catch secrets before they're committed |
| **Nightfall AI** | Free tier, paid from ~$10k+/yr | ML-based secret detection in agent output streams |

### Known Gaps in the Ecosystem

1. **No unified solution** covers all four layers (isolation + command policy + network egress + secret prevention) today
2. **Bypass vectors exist** in every native permission system tested (Claude Code deny-rule bypass, Cursor CVE-2026-22708)
3. **Domain allowlists are too coarse** — allowing `api.github.com` for git operations also allows commenting, creating issues, and dispatching workflows on the same domain. Method+path filtering (mitmproxy/Envoy) is required but adds operational complexity and TLS interception
4. **Exfiltration via allowed channels** (pushing secrets to permitted Git repos) remains fundamentally hard to prevent without restricting write access
5. **MCP servers are an unguarded channel** — most sandbox approaches focus on CLI/network but don't address MCP server tools that can send messages, create events, or post comments as the user
6. **Policy-as-code standards** are not yet established — each tool has its own configuration format

---

## References

- [Destructive Command Guard (DCG)](https://github.com/Dicklesworthstone/destructive_command_guard)
- [immunity-agent](https://github.com/PrismorSec/immunity-agent)
- [Microsoft Agent Governance Toolkit](https://github.com/microsoft/agent-governance-toolkit)
- [E2B Sandbox](https://e2b.dev)
- [Claude Code Permissions](https://code.claude.com/docs/en/permissions)
- [OpenAI Codex Security](https://developers.openai.com/codex/security)
- [Oktsec](https://github.com/oktsec/oktsec)
- [Pillar Security](https://pillar.security)
- [Lakera AI Agent Security](https://lakera.ai/ai-agent-security)
- [Galileo Agent Control](https://galileo.ai)
- [Nightfall AI](https://nightfall.ai)
- [Squid Proxy](http://www.squid-cache.org/)
- [mitmproxy](https://mitmproxy.org/)
- [Envoy Proxy](https://www.envoyproxy.io/)
- [Cilium — eBPF-based Networking](https://cilium.io/)
- [CVE-2026-22708 — Cursor Safe Mode Bypass](https://dev.to/cverports/cve-2026-22708-trust-issues-bypassing-cursor-ais-safe-mode-via-shell-built-ins-55ao)
- [Claude Code Deny Rule Bypass (Adversa AI)](https://adversa.ai/blog/claude-code-security-bypass-deny-rules-disabled/)
- [Wassette: WebAssembly-based Tools for AI Agents (Microsoft)](https://opensource.microsoft.com/blog/2025/08/06/introducing-wassette-webassembly-based-tools-for-ai-agents/)
- [Codex Agent Approvals & Security](https://developers.openai.com/codex/agent-approvals-security)
- [Human-in-the-Loop AI Agents (StackAI)](https://www.stackai.com/insights/human-in-the-loop-ai-agents-how-to-design-approval-workflows-for-safe-and-scalable-automation)
- [AI Agent Permissions — Delegated Access (Oso)](https://www.osohq.com/learn/ai-agent-permissions-delegated-access)
- [AI Agent Access Control (WorkOS)](https://workos.com/blog/ai-agent-access-control)
- [Access Control in the Era of AI Agents (Auth0)](https://auth0.com/blog/access-control-in-the-era-of-ai-agents/)
- [Infisical Agent Vault](https://github.com/Infisical/agent-vault)
- [SecretFS — FUSE Filesystem with Per-Process ACLs](https://github.com/obormot/secretfs)
- [ghtkn — Short-Lived GitHub App Tokens](https://github.com/suzuki-shunsuke/ghtkn)
- [abox — MicroVM Sandbox with Credential Proxy](https://github.com/X-McKay/abox)
- [botbox — K8s Sidecar with Credential Injection](https://github.com/reoring/botbox)
- [Using Proxies to Hide Secrets from Claude Code (Formal)](https://www.formal.ai/blog/using-proxies-claude-code/)
- [GCP Workload Identity Federation](https://docs.google.com/iam/docs/workload-identity-federation)
