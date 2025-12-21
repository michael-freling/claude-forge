package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/michael-freling/claude-code-tools/internal/command"
)

// Integration Test Documentation:
//
// The following methods in PRManager require git/gh CLI tools and are not unit testable:
//
// 1. CreatePR(ctx, title, body) - Requires:
//    - gh CLI tool installed
//    - Valid git repository with remote
//    - GitHub authentication configured
//    - Tests would create actual PRs on GitHub
//
// 2. GetCurrentBranchPR(ctx) - Requires:
//    - gh CLI tool installed
//    - Valid git repository with remote
//    - GitHub authentication configured
//    - Tests would query actual PRs from GitHub
//
// 3. EnsurePR(ctx, title, body) - Requires:
//    - Same as CreatePR and GetCurrentBranchPR combined
//
// 4. PushBranch(ctx) - Requires:
//    - git CLI tool installed
//    - Valid git repository with remote
//    - Write permissions to remote repository
//    - Tests would push to actual remote repository
//
// These methods should be tested with integration tests in a separate test suite
// that runs against a test repository with proper git/gh setup.

func TestNewPRManager(t *testing.T) {
	tests := []struct {
		name       string
		workingDir string
	}{
		{
			name:       "creates manager with working directory",
			workingDir: "/path/to/repo",
		},
		{
			name:       "creates manager with empty working directory",
			workingDir: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPRManager(tt.workingDir)
			require.NotNil(t, got)

			manager, ok := got.(*prManager)
			require.True(t, ok)
			assert.Equal(t, tt.workingDir, manager.workingDir)
		})
	}
}

func TestExtractPRNumberFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    int
		wantErr bool
	}{
		{
			name: "standard GitHub URL",
			url:  "https://github.com/owner/repo/pull/123",
			want: 123,
		},
		{
			name: "GitHub Enterprise URL",
			url:  "https://github.enterprise.com/owner/repo/pull/456",
			want: 456,
		},
		{
			name: "URL with trailing content",
			url:  "https://github.com/owner/repo/pull/789/files",
			want: 789,
		},
		{
			name: "URL with query params",
			url:  "https://github.com/owner/repo/pull/101?tab=files",
			want: 101,
		},
		{
			name: "URL with large PR number",
			url:  "https://github.com/owner/repo/pull/99999",
			want: 99999,
		},
		{
			name:    "invalid URL without PR number",
			url:     "https://github.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "invalid URL with non-numeric PR",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "URL without pull segment",
			url:     "https://github.com/owner/repo/issues/123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractPRNumberFromURL(tt.url)

			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, 0, got)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewPRManagerWithRunners(t *testing.T) {
	tests := []struct {
		name       string
		workingDir string
		gitRunner  command.GitRunner
		ghRunner   command.GhRunner
	}{
		{
			name:       "creates manager with mock runners",
			workingDir: "/test/repo",
			gitRunner:  &MockGitRunner{},
			ghRunner:   &MockGhRunner{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPRManagerWithRunners(tt.workingDir, tt.gitRunner, tt.ghRunner)
			require.NotNil(t, got)

			manager, ok := got.(*prManager)
			require.True(t, ok)
			assert.Equal(t, tt.workingDir, manager.workingDir)
			assert.Equal(t, tt.gitRunner, manager.gitRunner)
			assert.Equal(t, tt.ghRunner, manager.ghRunner)
		})
	}
}

func TestPRManager_CreatePR(t *testing.T) {
	tests := []struct {
		name         string
		title        string
		body         string
		setupGitMock func(*MockGitRunner)
		setupGhMock  func(*MockGhRunner)
		want         int
		wantErr      bool
		errContains  string
	}{
		{
			name:  "creates PR successfully",
			title: "Add new feature",
			body:  "This PR adds a new feature",
			setupGitMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("feature-branch", nil)
			},
			setupGhMock: func(m *MockGhRunner) {
				m.On("PRCreate", mock.Anything, "/test/repo", "Add new feature", "This PR adds a new feature", "feature-branch", "").
					Return("https://github.com/owner/repo/pull/123", nil)
			},
			want: 123,
		},
		{
			name:  "creates PR with different branch",
			title: "Fix bug",
			body:  "Bug fix description",
			setupGitMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("bugfix-branch", nil)
			},
			setupGhMock: func(m *MockGhRunner) {
				m.On("PRCreate", mock.Anything, "/test/repo", "Fix bug", "Bug fix description", "bugfix-branch", "").
					Return("https://github.com/owner/repo/pull/456", nil)
			},
			want: 456,
		},
		{
			name:  "fails when getting current branch fails",
			title: "Test PR",
			body:  "Test body",
			setupGitMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("", fmt.Errorf("not a git repository"))
			},
			setupGhMock: func(m *MockGhRunner) {},
			wantErr:     true,
			errContains: "failed to get current branch",
		},
		{
			name:  "fails when PR creation fails",
			title: "Test PR",
			body:  "Test body",
			setupGitMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("test-branch", nil)
			},
			setupGhMock: func(m *MockGhRunner) {
				m.On("PRCreate", mock.Anything, "/test/repo", "Test PR", "Test body", "test-branch", "").
					Return("", fmt.Errorf("failed to create PR"))
			},
			wantErr:     true,
			errContains: "failed to create PR",
		},
		{
			name:  "fails when PR URL is invalid",
			title: "Test PR",
			body:  "Test body",
			setupGitMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("test-branch", nil)
			},
			setupGhMock: func(m *MockGhRunner) {
				m.On("PRCreate", mock.Anything, "/test/repo", "Test PR", "Test body", "test-branch", "").
					Return("https://github.com/owner/repo/issues/123", nil)
			},
			wantErr:     true,
			errContains: "failed to extract PR number from URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGitRunner := new(MockGitRunner)
			mockGhRunner := new(MockGhRunner)
			tt.setupGitMock(mockGitRunner)
			tt.setupGhMock(mockGhRunner)

			manager := NewPRManagerWithRunners("/test/repo", mockGitRunner, mockGhRunner)
			ctx := context.Background()

			got, err := manager.CreatePR(ctx, tt.title, tt.body)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Equal(t, 0, got)
				mockGitRunner.AssertExpectations(t)
				mockGhRunner.AssertExpectations(t)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			mockGitRunner.AssertExpectations(t)
			mockGhRunner.AssertExpectations(t)
		})
	}
}

func TestPRManager_GetCurrentBranchPR(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockGhRunner)
		want        int
		wantErr     bool
		errContains string
	}{
		{
			name: "returns PR number successfully",
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("123", nil)
			},
			want: 123,
		},
		{
			name: "returns 0 when no PR found",
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("", fmt.Errorf("no pull requests found for branch"))
			},
			want: 0,
		},
		{
			name: "returns 0 when PR number is empty string",
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("", nil)
			},
			want: 0,
		},
		{
			name: "fails when PR number is invalid",
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("abc", nil)
			},
			wantErr:     true,
			errContains: "failed to parse PR number",
		},
		{
			name: "fails when gh command fails with other error",
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("", fmt.Errorf("GraphQL error"))
			},
			wantErr:     true,
			errContains: "GraphQL error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGhRunner := new(MockGhRunner)
			tt.setupMock(mockGhRunner)

			manager := NewPRManagerWithRunners("/test/repo", &MockGitRunner{}, mockGhRunner)
			ctx := context.Background()

			got, err := manager.GetCurrentBranchPR(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Equal(t, 0, got)
				mockGhRunner.AssertExpectations(t)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			mockGhRunner.AssertExpectations(t)
		})
	}
}

func TestPRManager_EnsurePR(t *testing.T) {
	tests := []struct {
		name         string
		title        string
		body         string
		setupGitMock func(*MockGitRunner)
		setupGhMock  func(*MockGhRunner)
		want         int
		wantErr      bool
		errContains  string
	}{
		{
			name:         "returns existing PR when found",
			title:        "Test PR",
			body:         "Test body",
			setupGitMock: func(m *MockGitRunner) {},
			setupGhMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("123", nil)
			},
			want: 123,
		},
		{
			name:  "creates new PR when none exists",
			title: "New PR",
			body:  "PR body",
			setupGitMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("feature-branch", nil)
			},
			setupGhMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("", fmt.Errorf("no pull requests found"))
				m.On("PRCreate", mock.Anything, "/test/repo", "New PR", "PR body", "feature-branch", "").
					Return("https://github.com/owner/repo/pull/456", nil)
			},
			want: 456,
		},
		{
			name:         "fails when checking for existing PR fails",
			title:        "Test PR",
			body:         "Test body",
			setupGitMock: func(m *MockGitRunner) {},
			setupGhMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("", fmt.Errorf("GraphQL error"))
			},
			wantErr:     true,
			errContains: "failed to check for existing PR",
		},
		{
			name:  "fails when creating new PR fails",
			title: "Test PR",
			body:  "Test body",
			setupGitMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("", fmt.Errorf("git error"))
			},
			setupGhMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 0, "number", ".number").
					Return("", fmt.Errorf("no pull requests found"))
			},
			wantErr:     true,
			errContains: "failed to get current branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGitRunner := new(MockGitRunner)
			mockGhRunner := new(MockGhRunner)
			tt.setupGitMock(mockGitRunner)
			tt.setupGhMock(mockGhRunner)

			manager := NewPRManagerWithRunners("/test/repo", mockGitRunner, mockGhRunner)
			ctx := context.Background()

			got, err := manager.EnsurePR(ctx, tt.title, tt.body)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Equal(t, 0, got)
				mockGitRunner.AssertExpectations(t)
				mockGhRunner.AssertExpectations(t)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			mockGitRunner.AssertExpectations(t)
			mockGhRunner.AssertExpectations(t)
		})
	}
}

func TestPRManager_PushBranch(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockGitRunner)
		wantErr     bool
		errContains string
	}{
		{
			name: "pushes branch successfully",
			setupMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("feature-branch", nil)
				m.On("Push", mock.Anything, "/test/repo", "feature-branch").
					Return(nil)
			},
		},
		{
			name: "pushes different branch successfully",
			setupMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("bugfix-branch", nil)
				m.On("Push", mock.Anything, "/test/repo", "bugfix-branch").
					Return(nil)
			},
		},
		{
			name: "fails when getting current branch fails",
			setupMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("", fmt.Errorf("not a git repository"))
			},
			wantErr:     true,
			errContains: "failed to get current branch",
		},
		{
			name: "fails when push fails",
			setupMock: func(m *MockGitRunner) {
				m.On("GetCurrentBranch", mock.Anything, "/test/repo").
					Return("test-branch", nil)
				m.On("Push", mock.Anything, "/test/repo", "test-branch").
					Return(fmt.Errorf("failed to push"))
			},
			wantErr:     true,
			errContains: "failed to push",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGitRunner := new(MockGitRunner)
			tt.setupMock(mockGitRunner)

			manager := NewPRManagerWithRunners("/test/repo", mockGitRunner, &MockGhRunner{})
			ctx := context.Background()

			err := manager.PushBranch(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				mockGitRunner.AssertExpectations(t)
				return
			}

			require.NoError(t, err)
			mockGitRunner.AssertExpectations(t)
		})
	}
}

func TestPRManager_ValidatePRForUpdate(t *testing.T) {
	tests := []struct {
		name        string
		prNumber    int
		setupMock   func(*MockGhRunner)
		want        *PRValidationResult
		wantErr     bool
		errContains string
	}{
		{
			name:     "validates open and mergeable PR successfully",
			prNumber: 123,
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 123, "number,state,headRefName,baseRefName,mergeable", ".").
					Return(`{"number":123,"state":"OPEN","headRefName":"feature-branch","baseRefName":"main","mergeable":"MERGEABLE"}`, nil)
			},
			want: &PRValidationResult{
				Number:      123,
				State:       "OPEN",
				HeadRefName: "feature-branch",
				BaseRefName: "main",
				Mergeable:   "MERGEABLE",
			},
		},
		{
			name:     "validates open PR with unknown mergeable status",
			prNumber: 456,
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 456, "number,state,headRefName,baseRefName,mergeable", ".").
					Return(`{"number":456,"state":"OPEN","headRefName":"bugfix","baseRefName":"main","mergeable":"UNKNOWN"}`, nil)
			},
			want: &PRValidationResult{
				Number:      456,
				State:       "OPEN",
				HeadRefName: "bugfix",
				BaseRefName: "main",
				Mergeable:   "UNKNOWN",
			},
		},
		{
			name:     "fails when PR number is zero",
			prNumber: 0,
			setupMock: func(m *MockGhRunner) {
			},
			wantErr:     true,
			errContains: "PR number must be positive",
		},
		{
			name:     "fails when PR number is negative",
			prNumber: -1,
			setupMock: func(m *MockGhRunner) {
			},
			wantErr:     true,
			errContains: "PR number must be positive",
		},
		{
			name:     "fails when gh command fails",
			prNumber: 123,
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 123, "number,state,headRefName,baseRefName,mergeable", ".").
					Return("", fmt.Errorf("PR not found"))
			},
			wantErr:     true,
			errContains: "failed to get PR info for #123",
		},
		{
			name:     "fails when JSON parsing fails",
			prNumber: 123,
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 123, "number,state,headRefName,baseRefName,mergeable", ".").
					Return(`{invalid json}`, nil)
			},
			wantErr:     true,
			errContains: "failed to parse PR info",
		},
		{
			name:     "fails when PR is closed",
			prNumber: 123,
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 123, "number,state,headRefName,baseRefName,mergeable", ".").
					Return(`{"number":123,"state":"CLOSED","headRefName":"feature","baseRefName":"main","mergeable":"MERGEABLE"}`, nil)
			},
			wantErr:     true,
			errContains: "PR #123 is CLOSED, cannot update",
		},
		{
			name:     "fails when PR is merged",
			prNumber: 456,
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 456, "number,state,headRefName,baseRefName,mergeable", ".").
					Return(`{"number":456,"state":"MERGED","headRefName":"feature","baseRefName":"main","mergeable":"MERGEABLE"}`, nil)
			},
			wantErr:     true,
			errContains: "PR #456 is MERGED, cannot update",
		},
		{
			name:     "fails when PR has conflicts",
			prNumber: 789,
			setupMock: func(m *MockGhRunner) {
				m.On("PRView", mock.Anything, "/test/repo", 789, "number,state,headRefName,baseRefName,mergeable", ".").
					Return(`{"number":789,"state":"OPEN","headRefName":"feature","baseRefName":"main","mergeable":"CONFLICTING"}`, nil)
			},
			wantErr:     true,
			errContains: "PR #789 has merge conflicts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGhRunner := new(MockGhRunner)
			tt.setupMock(mockGhRunner)

			manager := NewPRManagerWithRunners("/test/repo", &MockGitRunner{}, mockGhRunner)
			ctx := context.Background()

			got, err := manager.ValidatePRForUpdate(ctx, tt.prNumber)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, got)
				mockGhRunner.AssertExpectations(t)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.want.Number, got.Number)
			assert.Equal(t, tt.want.State, got.State)
			assert.Equal(t, tt.want.HeadRefName, got.HeadRefName)
			assert.Equal(t, tt.want.BaseRefName, got.BaseRefName)
			assert.Equal(t, tt.want.Mergeable, got.Mergeable)
			mockGhRunner.AssertExpectations(t)
		})
	}
}
