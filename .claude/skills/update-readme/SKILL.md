---
name: update-readme
description: Keep the end-user documentation — README.md (condensed overview) and the user guide under docs/content/docs/ (published to GitHub Pages) — in sync with the actual claude-forge CLI and configuration. Use after adding/removing/renaming a CLI command or flag, changing config fields or default images, or whenever the docs may have drifted from the code. Verifies every documented command, flag, config key, and image against the source of truth.
---

# Keep the end-user docs up to date

End-user documentation lives in two places that drift out of sync whenever the
CLI or config changes but the docs don't:

- `README.md` — a **condensed overview**: features, prerequisites,
  installation, quick start, and links to the docs site. No detailed command
  or config reference.
- `docs/content/docs/*.md` — the **full user guide**, published to GitHub
  Pages (usage, configuration, MCP servers, authentication, how-it-works).
  This is where the detailed reference lives. Ignore
  `docs/content/docs/development/` (internal, not user-facing).

This skill keeps both accurate by checking every claim against the source of
truth — never from memory.

## Sources of truth

| What the docs document | Where the truth lives |
| --- | --- |
| Commands, subcommands, flags, args | `cmd/claude-forge/main.go` (cobra `Use:`/`Short:`/`Long:`, `cmd.Flags()`, `Args:`) |
| Config keys and structure | `internal/forge/config/config.go` (struct `yaml:"..."` tags) |
| Default Docker image references | `internal/forge/config/config.go` (`Default*Image` constants) |
| Container architecture / lifecycle | `internal/forge/orchestrator.go`, `CLAUDE.md` |
| Installation / build | `go.mod` (module path, Go version), `Makefile` |

## Workflow

1. **Scope the change.** Identify what changed (a new command, a renamed flag,
   a new config field, a new image). If invoked proactively after a code edit,
   start from the diff.

2. **Enumerate the real CLI.** Read `cmd/claude-forge/main.go` and list every
   command registered in `newRootCmd()`'s `AddCommand(...)`, plus each
   subcommand's `Use:`, required `Args:`, and every `cmd.Flags()` flag (name,
   shorthand, default, description). Prefer building the binary and running it
   when feasible:

   ```bash
   go build -o /tmp/claude-forge ./cmd/claude-forge/
   /tmp/claude-forge --help
   /tmp/claude-forge <command> --help
   ```

3. **Enumerate config + images.** Read `internal/forge/config/config.go`. List
   every `yaml:"..."` key under `Config`, `ImagesConfig`, `DefaultsConfig`, and
   `MCPServerConfig`, and the `Default*Image` constant values.

4. **Diff against the docs.** Compare the lists to `README.md` and every page
   under `docs/content/docs/` (excluding `development/`). Flag:
   - Commands/flags documented but no longer present (remove them)
   - Commands/flags that exist but are undocumented (add them)
   - Wrong required arguments (e.g. `start` requires a `<name>`)
   - Stale config keys, image references, or example values
   - Examples that would error if run verbatim

5. **Update the docs.** Fix the discrepancies. Detailed reference material goes
   in the `docs/content/docs/` pages; the README only carries the overview,
   quick start, and links. Keep each file's existing structure, tone, and
   section order. Use realistic, copy-pasteable examples that match the real
   flags. Don't invent features that aren't in the code.

6. **Verify.** Re-read the sections you touched and re-run `--help` for any
   command involved to confirm the docs now match. If you changed pages under
   `docs/content/docs/`, build the site (`make docs-build`, or `hugo --source
   docs` with a Hugo extended binary) to catch broken relrefs.

## Guardrails

- **Never document from memory.** Every command, flag, default, and config key
  must trace to the current source. The CLI changes across versions.
- **Don't over-document internals.** `gateway` and similar
  container-use-only commands exist for the orchestrator, not end users —
  match the README's existing level of detail rather than dumping every
  internal subcommand.
- **Keep examples runnable.** If an example shows `claude-forge start`, make
  sure the required `<name>` argument is present.
- **Keep the README condensed.** Detailed command/config reference belongs in
  `docs/content/docs/`, not the README. If a README section starts growing a
  full reference, move the detail to the docs site and link to it.
- **One concern per change.** This skill maintains end-user docs (`README.md`
  and `docs/content/docs/`). Internal architecture/design docs live under
  `docs/content/docs/development/` and `CLAUDE.md`; update those separately
  when relevant.
