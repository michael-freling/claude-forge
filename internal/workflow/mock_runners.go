package workflow

import (
	"context"

	"github.com/michael-freling/claude-code-tools/internal/command"
	"github.com/stretchr/testify/mock"
)

// MockCommandRunner is a mock implementation of command.Runner
type MockCommandRunner struct {
	mock.Mock
}

// Ensure MockCommandRunner implements command.Runner
var _ command.Runner = (*MockCommandRunner)(nil)

func (m *MockCommandRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	callArgs := []interface{}{ctx, name}
	for _, arg := range args {
		callArgs = append(callArgs, arg)
	}
	mockArgs := m.Called(callArgs...)
	return mockArgs.String(0), mockArgs.String(1), mockArgs.Error(2)
}

func (m *MockCommandRunner) RunInDir(ctx context.Context, dir string, name string, args ...string) (string, string, error) {
	callArgs := []interface{}{ctx, dir, name}
	for _, arg := range args {
		callArgs = append(callArgs, arg)
	}
	mockArgs := m.Called(callArgs...)
	return mockArgs.String(0), mockArgs.String(1), mockArgs.Error(2)
}

// MockGitRunner is a mock implementation of command.GitRunner
type MockGitRunner struct {
	mock.Mock
}

// Ensure MockGitRunner implements command.GitRunner
var _ command.GitRunner = (*MockGitRunner)(nil)

func (m *MockGitRunner) GetCurrentBranch(ctx context.Context, dir string) (string, error) {
	args := m.Called(ctx, dir)
	return args.String(0), args.Error(1)
}

func (m *MockGitRunner) Push(ctx context.Context, dir string, branch string) error {
	args := m.Called(ctx, dir, branch)
	return args.Error(0)
}

func (m *MockGitRunner) WorktreeAdd(ctx context.Context, dir string, path string, branch string) error {
	args := m.Called(ctx, dir, path, branch)
	return args.Error(0)
}

func (m *MockGitRunner) WorktreeRemove(ctx context.Context, dir string, path string) error {
	args := m.Called(ctx, dir, path)
	return args.Error(0)
}

func (m *MockGitRunner) GetCommits(ctx context.Context, dir string, base string) ([]command.Commit, error) {
	args := m.Called(ctx, dir, base)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]command.Commit), args.Error(1)
}

func (m *MockGitRunner) CherryPick(ctx context.Context, dir string, commitHash string) error {
	args := m.Called(ctx, dir, commitHash)
	return args.Error(0)
}

func (m *MockGitRunner) CreateBranch(ctx context.Context, dir string, branchName string, baseBranch string) error {
	args := m.Called(ctx, dir, branchName, baseBranch)
	return args.Error(0)
}

func (m *MockGitRunner) CheckoutBranch(ctx context.Context, dir string, branchName string) error {
	args := m.Called(ctx, dir, branchName)
	return args.Error(0)
}

func (m *MockGitRunner) DeleteBranch(ctx context.Context, dir string, branchName string, force bool) error {
	args := m.Called(ctx, dir, branchName, force)
	return args.Error(0)
}

func (m *MockGitRunner) DeleteRemoteBranch(ctx context.Context, dir string, branchName string) error {
	args := m.Called(ctx, dir, branchName)
	return args.Error(0)
}

func (m *MockGitRunner) CommitEmpty(ctx context.Context, dir string, message string) error {
	args := m.Called(ctx, dir, message)
	return args.Error(0)
}

func (m *MockGitRunner) CheckoutFiles(ctx context.Context, dir string, sourceBranch string, files []string) error {
	args := m.Called(ctx, dir, sourceBranch, files)
	return args.Error(0)
}

func (m *MockGitRunner) CommitAll(ctx context.Context, dir string, message string) error {
	args := m.Called(ctx, dir, message)
	return args.Error(0)
}

func (m *MockGitRunner) GetDiffStat(ctx context.Context, dir string, base string) (string, error) {
	args := m.Called(ctx, dir, base)
	return args.String(0), args.Error(1)
}

func (m *MockGitRunner) FetchBranch(ctx context.Context, dir string, branch string) error {
	args := m.Called(ctx, dir, branch)
	return args.Error(0)
}

func (m *MockGitRunner) RemoteBranchExists(ctx context.Context, dir string, remote string, branch string) (bool, error) {
	args := m.Called(ctx, dir, remote, branch)
	return args.Bool(0), args.Error(1)
}

func (m *MockGitRunner) WorktreeAddFromBase(ctx context.Context, dir string, path string, branch string, baseBranch string) error {
	args := m.Called(ctx, dir, path, branch, baseBranch)
	return args.Error(0)
}

// MockGhRunner is a mock implementation of command.GhRunner
type MockGhRunner struct {
	mock.Mock
}

// Ensure MockGhRunner implements command.GhRunner
var _ command.GhRunner = (*MockGhRunner)(nil)

func (m *MockGhRunner) PRCreate(ctx context.Context, dir string, title, body, head, base string) (string, error) {
	args := m.Called(ctx, dir, title, body, head, base)
	return args.String(0), args.Error(1)
}

func (m *MockGhRunner) PREdit(ctx context.Context, dir string, prNumber int, body string) error {
	args := m.Called(ctx, dir, prNumber, body)
	return args.Error(0)
}

func (m *MockGhRunner) PRClose(ctx context.Context, dir string, prNumber int) error {
	args := m.Called(ctx, dir, prNumber)
	return args.Error(0)
}

func (m *MockGhRunner) PRView(ctx context.Context, dir string, prNumber int, jsonFields string, jqQuery string) (string, error) {
	args := m.Called(ctx, dir, prNumber, jsonFields, jqQuery)
	return args.String(0), args.Error(1)
}

func (m *MockGhRunner) PRChecks(ctx context.Context, dir string, prNumber int, jsonFields string) (string, error) {
	args := m.Called(ctx, dir, prNumber, jsonFields)
	return args.String(0), args.Error(1)
}

func (m *MockGhRunner) GetPRBaseBranch(ctx context.Context, dir string, prNumber string) (string, error) {
	args := m.Called(ctx, dir, prNumber)
	return args.String(0), args.Error(1)
}

func (m *MockGhRunner) RunRerun(ctx context.Context, dir string, runID int64) error {
	args := m.Called(ctx, dir, runID)
	return args.Error(0)
}

func (m *MockGhRunner) GetLatestRunID(ctx context.Context, dir string, prNumber int) (int64, error) {
	args := m.Called(ctx, dir, prNumber)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockGhRunner) ListPRs(ctx context.Context, dir string, branch string) ([]command.PRInfo, error) {
	args := m.Called(ctx, dir, branch)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]command.PRInfo), args.Error(1)
}

// MockPRManager is a mock implementation of PRManager
type MockPRManager struct {
	mock.Mock
}

// Ensure MockPRManager implements PRManager
var _ PRManager = (*MockPRManager)(nil)

func (m *MockPRManager) CreatePR(ctx context.Context, title, body string) (int, error) {
	args := m.Called(ctx, title, body)
	return args.Int(0), args.Error(1)
}

func (m *MockPRManager) GetCurrentBranchPR(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockPRManager) EnsurePR(ctx context.Context, title, body string) (int, error) {
	args := m.Called(ctx, title, body)
	return args.Int(0), args.Error(1)
}

func (m *MockPRManager) PushBranch(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockPRManager) ValidatePRForUpdate(ctx context.Context, prNumber int) (*PRValidationResult, error) {
	args := m.Called(ctx, prNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*PRValidationResult), args.Error(1)
}
