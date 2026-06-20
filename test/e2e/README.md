# E2E Testing

This directory contains end-to-end (e2e) tests that verify complete workflows using the REAL Claude CLI.

## Overview

E2E tests are separated from unit tests using Go build tags (`//go:build e2e`). These tests:

- **Use REAL Claude CLI** - Tests execute actual Claude commands (not mocks)
- **Run locally only** - Skipped in CI since they require Claude authentication
- **Test real integration** - Verify the actual Claude API integration works
- **Are intentionally slow** - Real Claude calls take 30s-2min per phase
- **Cost real money** - Each test makes actual API calls to Claude

## Prerequisites

### Required Tools

- **git** - Required for all e2e tests
- **Go** - For running the tests
- **claude** - Claude Code CLI (required for E2E tests)

### Claude CLI Installation and Authentication

E2E tests require Claude CLI to be installed and authenticated:

```bash
# Install claude CLI
# See https://docs.anthropic.com/en/docs/claude-code

# Authenticate (if needed)
# The CLI will prompt for authentication on first use

# Verify Claude is available
claude --version
```

Tests will be automatically skipped if Claude is not available in PATH.

## Running E2E Tests

### Using the Script (Recommended)

```bash
# Run all e2e tests
./scripts/run-e2e-tests.sh

# Run with verbose output
E2E_VERBOSE=true ./scripts/run-e2e-tests.sh

# Run with custom timeout (E2E tests can be slow!)
E2E_TIMEOUT=10m ./scripts/run-e2e-tests.sh
```

### Using go test Directly

```bash
# Run all e2e tests
go test -tags=e2e ./test/e2e/...

# Run with verbose output
go test -tags=e2e -v ./test/e2e/...

# Use longer timeout for slow real Claude calls
go test -tags=e2e -timeout=10m ./test/e2e/...
```

## Test Files

### Real CLI Tests

- `claude_cli_e2e_test.go` - Tests low-level Claude CLI execution
  - Simple execution and streaming modes
  - JSON schema validation
  - Working directory file access

### Helpers

- `helpers/` - Test helper utilities
  - `claude.go` - Claude CLI detection and availability checks
  - `repo.go` - Temporary Git repository management and cloning
  - `git.go`, `gh.go` - Git and GitHub CLI test utilities
  - `cleanup.go` - Resource cleanup helpers

Note: `mock_claude.go` is still available for unit tests but is NOT used in E2E tests.

## Writing E2E Tests

### Basic E2E Test Template

E2E tests should use the REAL Claude CLI, not mocks:

```go
//go:build e2e

package e2e

import (
	"testing"

	"github.com/michael-freling/claude-forge/test/e2e/helpers"
	"github.com/stretchr/testify/require"
)

func TestMyFeature_E2E(t *testing.T) {
	// REQUIRED: Skip if Claude not available
	helpers.RequireClaude(t)
	helpers.RequireGit(t)

	// Create real temp repo
	repo := helpers.NewTempRepo(t)
	require.NoError(t, repo.CreateFile("main.go", "package main\n\nfunc main() {}\n"))
	require.NoError(t, repo.Commit("Initial commit"))

	// Run your test with real Claude CLI
	// ...
}
```

### Key Principles for E2E Tests

1. **Always use real Claude** - No MockClaudeBuilder in E2E tests
2. **Always use `helpers.RequireClaude(t)`** - Skip when Claude not available
3. **Keep prompts simple** - Minimize execution time and API costs
4. **Use generous timeouts** - Real Claude is slow (30s-5min per operation)
5. **Add helpful logging** - Use `t.Logf()` to track progress

### Skip Functions

```go
// Skip if git not available
helpers.RequireGit(t)

// Skip if gh not available
helpers.RequireGH(t)

// Skip if gh not authenticated
helpers.RequireGHAuth(t)

// Skip if claude not available (REQUIRED for E2E tests)
helpers.RequireClaude(t)

// Check claude availability without skipping
if helpers.IsCLIAvailable() {
	// claude is available
}
```

### Best Practices

1. **Always use build tags**: Start every e2e test file with `//go:build e2e`
2. **Use real Claude**: No MockClaudeBuilder in E2E tests
3. **Check prerequisites**: Always use `helpers.RequireClaude(t)` at the start
4. **Simple prompts**: Keep descriptions minimal to save time and money
5. **Verbose logging**: Enable `LogLevelVerbose` to debug issues
6. **Descriptive names**: Use clear test names that indicate they're real E2E tests
7. **Independence**: Each test should be independent
8. **Resource cleanup**: Tests automatically cleanup temp repos and worktrees

## Troubleshooting

### Tests are skipped

If tests are being skipped, check that Claude CLI is installed and in PATH:

```bash
# Check claude
claude --version

# If not found, install Claude CLI
# See https://docs.anthropic.com/en/docs/claude-code
```

### Authentication errors

If you see authentication errors:

```bash
# The Claude CLI will prompt for authentication on first use
# Just run any claude command and follow the prompts
claude "hello"
```

### Timeout errors

Real Claude calls can be slow. If tests timeout, increase the timeout:

```bash
# Using script
E2E_TIMEOUT=15m ./scripts/run-e2e-tests.sh

# Using go test
go test -tags=e2e -timeout=15m ./test/e2e/...
```

### Permission errors

If you see permission errors when creating temporary directories:

```bash
# Check temp directory permissions
ls -la /tmp

# Set custom temp directory
export TMPDIR=/path/to/writable/dir
```

### Git configuration issues

If git operations fail with user configuration errors:

```bash
# Set global git config
git config --global user.email "test@test.com"
git config --global user.name "Test User"
```

Note: The TempRepo helper automatically configures git user for each test repository.

## CI Integration

**E2E tests are NOT run in CI** because they require:
- Claude CLI authentication
- Real API access
- Significant time (3-10min per test)
- API costs

These tests are designed for local development and manual verification only.

## Cost Considerations

Each E2E test makes real API calls to Claude, which costs money.

**Minimize costs by:**
- Using simple, minimal descriptions
- Running tests selectively (not the full suite repeatedly)
- Testing only when you need to verify real Claude integration

## Related Documentation

- [Go Testing](https://golang.org/pkg/testing/)
- [Build Tags](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
- [testify](https://github.com/stretchr/testify)
- [Claude Code Documentation](https://docs.anthropic.com/en/docs/claude-code)
