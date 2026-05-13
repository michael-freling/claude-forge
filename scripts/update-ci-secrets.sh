#!/bin/bash
set -euo pipefail

REPO="michael-freling/claude-code-tools"

usage() {
    cat <<EOF
Usage: $0 [--oauth-token TOKEN] [--from-credentials]

Update GitHub Actions secrets for CI.

Options:
  --oauth-token TOKEN    Set CLAUDE_CODE_OAUTH_TOKEN directly
  --from-credentials     Read token from ~/.claude/.credentials.json
  -h, --help             Show this help

Examples:
  # Set token directly
  $0 --oauth-token "your-token-here"

  # Read from Claude Code credentials file
  $0 --from-credentials
EOF
    exit 0
}

set_oauth_token() {
    local token="$1"
    if [ -z "$token" ]; then
        echo "Error: empty token" >&2
        exit 1
    fi

    local masked="${token:0:8}...${token: -4}"
    echo "Setting CLAUDE_CODE_OAUTH_TOKEN ($masked) on $REPO"
    echo "$token" | gh secret set CLAUDE_CODE_OAUTH_TOKEN --repo "$REPO"
    echo "Done."
}

read_credentials_file() {
    local creds_file="$HOME/.claude/.credentials.json"
    if [ ! -f "$creds_file" ]; then
        echo "Error: $creds_file not found" >&2
        echo "Run 'claude' to authenticate first." >&2
        exit 1
    fi

    local token
    # Try nested format (claudeAiOauth.accessToken)
    token=$(jq -r '.claudeAiOauth.accessToken // empty' "$creds_file" 2>/dev/null)
    if [ -z "$token" ]; then
        # Fall back to legacy flat format
        token=$(jq -r '.accessToken // empty' "$creds_file" 2>/dev/null)
    fi

    if [ -z "$token" ]; then
        echo "Error: no accessToken found in $creds_file" >&2
        exit 1
    fi

    echo "$token"
}

if [ $# -eq 0 ]; then
    usage
fi

while [ $# -gt 0 ]; do
    case "$1" in
        --oauth-token)
            shift
            set_oauth_token "$1"
            exit 0
            ;;
        --from-credentials)
            token=$(read_credentials_file)
            set_oauth_token "$token"
            exit 0
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1" >&2
            usage
            ;;
    esac
    shift
done
