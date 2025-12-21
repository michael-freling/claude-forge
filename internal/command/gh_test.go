package command

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewGhRunner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRunner := NewMockRunner(ctrl)
	got := NewGhRunner(mockRunner)

	require.NotNil(t, got)
}

func TestGhRunner_PRCreate(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		title       string
		body        string
		head        string
		base        string
		setupMock   func(*MockRunner)
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:  "creates PR successfully without base branch",
			dir:   "/test/repo",
			title: "Test PR",
			body:  "Test body",
			head:  "feature-branch",
			base:  "",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "create", "--title", "Test PR", "--body", "Test body", "--head", "feature-branch").
					Return("https://github.com/owner/repo/pull/123\n", "", nil)
			},
			want:    "https://github.com/owner/repo/pull/123",
			wantErr: false,
		},
		{
			name:  "creates PR successfully with base branch",
			dir:   "/test/repo",
			title: "Test PR",
			body:  "Test body",
			head:  "feature-branch",
			base:  "develop",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "create", "--title", "Test PR", "--body", "Test body", "--head", "feature-branch", "--base", "develop").
					Return("https://github.com/owner/repo/pull/123\n", "", nil)
			},
			want:    "https://github.com/owner/repo/pull/123",
			wantErr: false,
		},
		{
			name:  "fails when gh command fails",
			dir:   "/test/repo",
			title: "Test PR",
			body:  "Test body",
			head:  "feature-branch",
			base:  "",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "create", "--title", "Test PR", "--body", "Test body", "--head", "feature-branch").
					Return("", "error: failed to create PR", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to create PR",
		},
		{
			name:        "fails when title is empty",
			dir:         "/test/repo",
			title:       "",
			body:        "Test body",
			head:        "feature-branch",
			base:        "",
			setupMock:   func(_ *MockRunner) {},
			wantErr:     true,
			errContains: "title cannot be empty",
		},
		{
			name:        "fails when head is empty",
			dir:         "/test/repo",
			title:       "Test PR",
			body:        "Test body",
			head:        "",
			base:        "",
			setupMock:   func(_ *MockRunner) {},
			wantErr:     true,
			errContains: "head branch cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			got, err := ghRunner.PRCreate(ctx, tt.dir, tt.title, tt.body, tt.head, tt.base)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGhRunner_PRView(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		prNumber    int
		jsonFields  string
		jqQuery     string
		setupMock   func(*MockRunner)
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:       "views current branch PR successfully",
			dir:        "/test/repo",
			prNumber:   0,
			jsonFields: "number,title",
			jqQuery:    ".number",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "view", "--json", "number,title", "-q", ".number").
					Return("123", "", nil)
			},
			want:    "123",
			wantErr: false,
		},
		{
			name:       "views specific PR by number",
			dir:        "/test/repo",
			prNumber:   456,
			jsonFields: "number,state",
			jqQuery:    ".",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "view", "456", "--json", "number,state", "-q", ".").
					Return(`{"number":456,"state":"OPEN"}`, "", nil)
			},
			want:    `{"number":456,"state":"OPEN"}`,
			wantErr: false,
		},
		{
			name:       "fails when gh command fails",
			dir:        "/test/repo",
			prNumber:   0,
			jsonFields: "number,title",
			jqQuery:    ".number",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "view", "--json", "number,title", "-q", ".number").
					Return("", "error: failed to view PR", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to view PR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			got, err := ghRunner.PRView(ctx, tt.dir, tt.prNumber, tt.jsonFields, tt.jqQuery)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGhRunner_PRChecks(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		prNumber    int
		jsonFields  string
		setupMock   func(*MockRunner)
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:       "checks PR status with PR number",
			dir:        "/test/repo",
			prNumber:   123,
			jsonFields: "state,conclusion",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "checks", "123", "--json", "state,conclusion").
					Return(`[{"state":"success","conclusion":"success"}]`, "", nil)
			},
			want:    `[{"state":"success","conclusion":"success"}]`,
			wantErr: false,
		},
		{
			name:       "checks PR status without PR number",
			dir:        "/test/repo",
			prNumber:   0,
			jsonFields: "state,conclusion",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "checks", "--json", "state,conclusion").
					Return(`[{"state":"success","conclusion":"success"}]`, "", nil)
			},
			want:    `[{"state":"success","conclusion":"success"}]`,
			wantErr: false,
		},
		{
			name:       "fails when gh command fails",
			dir:        "/test/repo",
			prNumber:   123,
			jsonFields: "state,conclusion",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "checks", "123", "--json", "state,conclusion").
					Return("", "error: failed to check PR status", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to check PR status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			got, err := ghRunner.PRChecks(ctx, tt.dir, tt.prNumber, tt.jsonFields)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGhRunner_GetPRBaseBranch(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		prNumber    string
		setupMock   func(*MockRunner)
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:     "gets base branch successfully",
			dir:      "/test/repo",
			prNumber: "123",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "view", "123", "--json", "baseRefName", "--jq", ".baseRefName").
					Return("main", "", nil)
			},
			want:    "main",
			wantErr: false,
		},
		{
			name:     "trims whitespace from branch name",
			dir:      "/test/repo",
			prNumber: "123",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "view", "123", "--json", "baseRefName", "--jq", ".baseRefName").
					Return("  develop  ", "", nil)
			},
			want:    "develop",
			wantErr: false,
		},
		{
			name:     "fails when gh command fails",
			dir:      "/test/repo",
			prNumber: "123",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "view", "123", "--json", "baseRefName", "--jq", ".baseRefName").
					Return("", "error: pull request not found", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to get PR base branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			got, err := ghRunner.GetPRBaseBranch(ctx, tt.dir, tt.prNumber)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGhRunner_RunRerun(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		runID       int64
		setupMock   func(*MockRunner)
		wantErr     bool
		errContains string
	}{
		{
			name:  "reruns workflow successfully",
			dir:   "/test/repo",
			runID: 123456,
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "run", "rerun", "123456", "--failed").
					Return("", "", nil)
			},
			wantErr: false,
		},
		{
			name:  "fails when gh command fails",
			dir:   "/test/repo",
			runID: 123456,
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "run", "rerun", "123456", "--failed").
					Return("", "error: workflow run not found", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to rerun workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			err := ghRunner.RunRerun(ctx, tt.dir, tt.runID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestGhRunner_GetLatestRunID(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		prNumber    int
		setupMock   func(*MockRunner)
		want        int64
		wantErr     bool
		errContains string
	}{
		{
			name:     "gets latest run ID successfully",
			dir:      "/test/repo",
			prNumber: 123,
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "checks", "123", "--json", "databaseId").
					Return(`[{"databaseId":123456},{"databaseId":123455}]`, "", nil)
			},
			want:    123456,
			wantErr: false,
		},
		{
			name:     "fails when no workflow runs found",
			dir:      "/test/repo",
			prNumber: 123,
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "checks", "123", "--json", "databaseId").
					Return(`[]`, "", nil)
			},
			wantErr:     true,
			errContains: "no workflow runs found",
		},
		{
			name:     "fails when gh command fails",
			dir:      "/test/repo",
			prNumber: 123,
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "checks", "123", "--json", "databaseId").
					Return("", "error: pull request not found", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to get workflow run ID",
		},
		{
			name:     "fails when JSON is invalid",
			dir:      "/test/repo",
			prNumber: 123,
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "checks", "123", "--json", "databaseId").
					Return(`invalid json`, "", nil)
			},
			wantErr:     true,
			errContains: "failed to parse workflow run ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			got, err := ghRunner.GetLatestRunID(ctx, tt.dir, tt.prNumber)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGhRunner_PREdit(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		prNumber    int
		body        string
		setupMock   func(*MockRunner)
		wantErr     bool
		errContains string
	}{
		{
			name:     "edits PR successfully",
			dir:      "/test/repo",
			prNumber: 123,
			body:     "Updated PR body",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "edit", "123", "--body", "Updated PR body").
					Return("", "", nil)
			},
			wantErr: false,
		},
		{
			name:     "edits PR with empty body",
			dir:      "/test/repo",
			prNumber: 456,
			body:     "",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "edit", "456", "--body", "").
					Return("", "", nil)
			},
			wantErr: false,
		},
		{
			name:        "fails when PR number is zero",
			dir:         "/test/repo",
			prNumber:    0,
			body:        "Updated body",
			setupMock:   func(m *MockRunner) {},
			wantErr:     true,
			errContains: "PR number must be positive",
		},
		{
			name:        "fails when PR number is negative",
			dir:         "/test/repo",
			prNumber:    -1,
			body:        "Updated body",
			setupMock:   func(m *MockRunner) {},
			wantErr:     true,
			errContains: "PR number must be positive",
		},
		{
			name:     "fails when gh command fails",
			dir:      "/test/repo",
			prNumber: 123,
			body:     "Updated body",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "edit", "123", "--body", "Updated body").
					Return("", "error: pull request not found", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to edit PR 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			err := ghRunner.PREdit(ctx, tt.dir, tt.prNumber, tt.body)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestGhRunner_PRClose(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		prNumber    int
		setupMock   func(*MockRunner)
		wantErr     bool
		errContains string
	}{
		{
			name:     "closes PR successfully",
			dir:      "/test/repo",
			prNumber: 123,
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "close", "123").
					Return("", "", nil)
			},
			wantErr: false,
		},
		{
			name:        "fails when PR number is zero",
			dir:         "/test/repo",
			prNumber:    0,
			setupMock:   func(m *MockRunner) {},
			wantErr:     true,
			errContains: "PR number must be positive",
		},
		{
			name:        "fails when PR number is negative",
			dir:         "/test/repo",
			prNumber:    -5,
			setupMock:   func(m *MockRunner) {},
			wantErr:     true,
			errContains: "PR number must be positive",
		},
		{
			name:     "fails when gh command fails",
			dir:      "/test/repo",
			prNumber: 123,
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "close", "123").
					Return("", "error: pull request not found", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to close PR 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			err := ghRunner.PRClose(ctx, tt.dir, tt.prNumber)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestGhRunner_ListPRs(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		branch      string
		setupMock   func(*MockRunner)
		want        []PRInfo
		wantErr     bool
		errContains string
	}{
		{
			name:   "lists PRs successfully",
			dir:    "/test/repo",
			branch: "feature-branch",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "list", "--head", "feature-branch", "--json", "number,url,title,headRefName", "--limit", "1").
					Return(`[{"number":123,"url":"https://github.com/owner/repo/pull/123","title":"Test PR","headRefName":"feature-branch"}]`, "", nil)
			},
			want: []PRInfo{
				{
					Number:      123,
					URL:         "https://github.com/owner/repo/pull/123",
					Title:       "Test PR",
					HeadRefName: "feature-branch",
				},
			},
			wantErr: false,
		},
		{
			name:   "returns empty slice when no PRs found",
			dir:    "/test/repo",
			branch: "feature-branch",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "list", "--head", "feature-branch", "--json", "number,url,title,headRefName", "--limit", "1").
					Return(`[]`, "", nil)
			},
			want:    []PRInfo{},
			wantErr: false,
		},
		{
			name:        "fails when branch is empty",
			dir:         "/test/repo",
			branch:      "",
			setupMock:   func(m *MockRunner) {},
			wantErr:     true,
			errContains: "branch cannot be empty",
		},
		{
			name:   "fails when gh command fails",
			dir:    "/test/repo",
			branch: "feature-branch",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "list", "--head", "feature-branch", "--json", "number,url,title,headRefName", "--limit", "1").
					Return("", "error: failed to list PRs", fmt.Errorf("exit status 1"))
			},
			wantErr:     true,
			errContains: "failed to list PRs for branch feature-branch",
		},
		{
			name:   "fails when JSON is invalid",
			dir:    "/test/repo",
			branch: "feature-branch",
			setupMock: func(m *MockRunner) {
				m.EXPECT().
					RunInDir(gomock.Any(), "/test/repo", "gh", "pr", "list", "--head", "feature-branch", "--json", "number,url,title,headRefName", "--limit", "1").
					Return(`invalid json`, "", nil)
			},
			wantErr:     true,
			errContains: "failed to parse PR list from output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRunner := NewMockRunner(ctrl)
			tt.setupMock(mockRunner)

			ghRunner := NewGhRunner(mockRunner)
			ctx := context.Background()

			got, err := ghRunner.ListPRs(ctx, tt.dir, tt.branch)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
