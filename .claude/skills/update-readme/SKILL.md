---
name: update-readme
description: Keep README.md, the end-user documentation, in sync with the actual claude-forge CLI and configuration. Use after adding/removing/renaming a CLI command or flag, changing config fields or default images, or whenever the README may have drifted from the code. Verifies every documented command, flag, config key, and image against the source of truth.
---

# Keep the README up to date

`README.md` is the primary end-user documentation for claude-forge. It drifts
out of sync whenever the CLI or config changes but the docs don't. This skill
keeps it accurate by checking every claim against the source of truth — never
from memory.

## Sources of truth

| What the README documents | Where the truth lives |
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
   `KubernetesConfig`, and the `Default*Image` constant values.

4. **Diff against the README.** Compare the lists to `README.md`. Flag:
   - Commands/flags documented but no longer present (remove them)
   - Commands/flags that exist but are undocumented (add them)
   - Wrong required arguments (e.g. `start` requires a `<name>`)
   - Stale config keys, image references, or example values
   - Examples that would error if run verbatim

5. **Update `README.md`.** Fix the discrepancies. Keep the existing structure,
   tone, and section order. Use realistic, copy-pasteable examples that match
   the real flags. Don't invent features that aren't in the code.

6. **Verify.** Re-read the relevant README sections and re-run `--help` for any
   command you touched to confirm the docs now match.

## Guardrails

- **Never document from memory.** Every command, flag, default, and config key
  must trace to the current source. The CLI changes across versions.
- **Don't over-document internals.** `gateway` and similar
  container-use-only commands exist for the orchestrator, not end users —
  match the README's existing level of detail rather than dumping every
  internal subcommand.
- **Keep examples runnable.** If an example shows `claude-forge start`, make
  sure the required `<name>` argument is present.
- **One concern per change.** This skill maintains end-user docs in
  `README.md`. Architecture/design docs live under `docs/` and `CLAUDE.md`;
  update those separately when relevant.
