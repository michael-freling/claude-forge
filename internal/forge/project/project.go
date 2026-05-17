package project

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Project holds metadata about a project derived from its git remote.
type Project struct {
	ID    string // derived from host dir path, e.g. "-home-user-my-project"
	Owner string // GitHub owner from remote URL
	Repo  string // GitHub repo name from remote URL
	Dir   string // host directory path
}

// sshRemoteRegexp matches SSH remote URLs like git@github.com:owner/repo.git
var sshRemoteRegexp = regexp.MustCompile(`^git@[^:]+:([^/]+)/([^/]+?)(?:\.git)?$`)

// httpsRemoteRegexp matches HTTPS remote URLs like https://github.com/owner/repo.git
// Also matches gateway-proxied URLs like http://gateway:8080/github.com/owner/repo.git
var httpsRemoteRegexp = regexp.MustCompile(`^https?://[^/]+/(?:github\.com/)?([^/]+)/([^/]+?)(?:\.git)?$`)

// GitConfig reads a git config value from the host's git configuration.
// It returns an empty string if the key is not set or git is not available.
func GitConfig(key string) string {
	cmd := exec.Command("git", "config", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Identify extracts project info from a directory by reading its git remote.
// dir is the host path to the project directory.
// It runs `git -C <dir> remote get-url origin` and parses the result.
// Supports SSH (git@github.com:owner/repo.git) and HTTPS (https://github.com/owner/repo.git) URLs.
// Project ID is derived by replacing "/" with "-" in the dir path.
func Identify(dir string) (*Project, error) {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get git remote URL: %w", err)
	}

	remoteURL := strings.TrimSpace(string(output))
	owner, repo, err := parseRemoteURL(remoteURL)
	if err != nil {
		return nil, err
	}

	return &Project{
		ID:    strings.ReplaceAll(dir, "/", "-"),
		Owner: owner,
		Repo:  repo,
		Dir:   dir,
	}, nil
}

// parseRemoteURL parses a git remote URL and returns the owner and repo name.
func parseRemoteURL(remoteURL string) (string, string, error) {
	if matches := sshRemoteRegexp.FindStringSubmatch(remoteURL); matches != nil {
		return matches[1], matches[2], nil
	}

	if matches := httpsRemoteRegexp.FindStringSubmatch(remoteURL); matches != nil {
		return matches[1], matches[2], nil
	}

	return "", "", fmt.Errorf("unsupported remote URL format: %s", remoteURL)
}
