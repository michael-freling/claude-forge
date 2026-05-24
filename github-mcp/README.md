# github-mcp

A GitHub MCP (Model Context Protocol) server that runs as a per-session sidecar container in claude-forge. It exposes GitHub API operations as MCP tools via Streamable HTTP, with owner/repo policy enforcement.

## Tools

| Tool | Description | Type |
|------|-------------|------|
| `github_pr_list` | List pull requests | read |
| `github_pr_get` | Get a pull request | read |
| `github_pr_create` | Create a pull request | write |
| `github_pr_update` | Update a pull request | write |
| `github_pr_merge` | Merge a pull request | write |
| `github_pr_comment` | Comment on a pull request | write |
| `github_pr_reviews` | List PR reviews | read |
| `github_issue_list` | List issues | read |
| `github_issue_get` | Get an issue | read |
| `github_issue_create` | Create an issue | write |
| `github_issue_comment` | Comment on an issue | write |
| `github_repo_get` | Get repository info | read |
| `github_release_list` | List releases | read |
| `github_checks_list` | List check runs for a ref | read |
| `github_api` | Generic GitHub API call | varies |

## Policy

- **Read** operations are allowed on any repository.
- **Write** operations are restricted to the configured `--owner`/`--repo`.

## Usage

```
github-mcp --owner <owner> --repo <repo> [--addr :8083]
```

Auth is via `GITHUB_TOKEN` environment variable or Bearer token in the `Authorization` header.

## Docker

```
docker build -t github-mcp .
docker run -e GITHUB_TOKEN=ghp_... github-mcp --owner myorg --repo myrepo
```

## Development

```
cd github-mcp
go test -v -race ./...
```
