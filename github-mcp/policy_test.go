package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicy_ReadAlwaysAllowed(t *testing.T) {
	p := &Policy{AllowedOwner: "my-owner", AllowedRepo: "my-repo"}

	tests := []struct {
		name  string
		tool  string
		owner string
		repo  string
	}{
		{"same repo", "github_pr_list", "my-owner", "my-repo"},
		{"different repo", "github_pr_list", "other-owner", "other-repo"},
		{"different owner same repo", "github_issue_get", "other-owner", "my-repo"},
		{"same owner different repo", "github_repo_get", "my-owner", "other-repo"},
		{"empty owner/repo", "github_pr_list", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.CheckTool(tt.tool, false, tt.owner, tt.repo)
			assert.NoError(t, err)
		})
	}
}

func TestPolicy_WriteAllowedForMatchingRepo(t *testing.T) {
	p := &Policy{AllowedOwner: "my-owner", AllowedRepo: "my-repo"}

	err := p.CheckTool("github_pr_create", true, "my-owner", "my-repo")
	assert.NoError(t, err)
}

func TestPolicy_WriteDeniedForDifferentRepo(t *testing.T) {
	p := &Policy{AllowedOwner: "my-owner", AllowedRepo: "my-repo"}

	tests := []struct {
		name  string
		owner string
		repo  string
	}{
		{"different owner and repo", "other-owner", "other-repo"},
		{"same owner different repo", "my-owner", "other-repo"},
		{"different owner same repo", "other-owner", "my-repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.CheckTool("github_pr_create", true, tt.owner, tt.repo)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "write access denied")
		})
	}
}

func TestPolicy_CaseInsensitiveMatching(t *testing.T) {
	p := &Policy{AllowedOwner: "My-Owner", AllowedRepo: "My-Repo"}

	tests := []struct {
		name  string
		owner string
		repo  string
	}{
		{"lowercase", "my-owner", "my-repo"},
		{"uppercase", "MY-OWNER", "MY-REPO"},
		{"mixed case", "mY-oWnEr", "mY-rEpO"},
		{"exact case", "My-Owner", "My-Repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.CheckTool("github_pr_create", true, tt.owner, tt.repo)
			assert.NoError(t, err)
		})
	}
}

func TestPolicy_GitHubApiReadVsWrite(t *testing.T) {
	p := &Policy{AllowedOwner: "my-owner", AllowedRepo: "my-repo"}

	// Read (isWrite=false) on different repo should be allowed
	err := p.CheckTool("github_api", false, "other-owner", "other-repo")
	assert.NoError(t, err)

	// Write (isWrite=true) on different repo should be denied
	err = p.CheckTool("github_api", true, "other-owner", "other-repo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write access denied")

	// Write (isWrite=true) on allowed repo should be allowed
	err = p.CheckTool("github_api", true, "my-owner", "my-repo")
	assert.NoError(t, err)
}
