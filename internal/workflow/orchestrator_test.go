package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/michael-freling/claude-code-tools/internal/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// MockClaudeExecutor is a mock implementation of ClaudeExecutor
type MockClaudeExecutor struct {
	mock.Mock
}

func (m *MockClaudeExecutor) Execute(ctx context.Context, config ExecuteConfig) (*ExecuteResult, error) {
	args := m.Called(ctx, config)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ExecuteResult), args.Error(1)
}

func (m *MockClaudeExecutor) ExecuteStreaming(ctx context.Context, config ExecuteConfig, onProgress func(ProgressEvent)) (*ExecuteResult, error) {
	args := m.Called(ctx, config, onProgress)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ExecuteResult), args.Error(1)
}

// MockStateManager is a mock implementation of StateManager
type MockStateManager struct {
	mock.Mock
}

func (m *MockStateManager) EnsureWorkflowDir(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockStateManager) WorkflowExists(name string) bool {
	args := m.Called(name)
	return args.Bool(0)
}

func (m *MockStateManager) WorkflowDir(name string) string {
	args := m.Called(name)
	return args.String(0)
}

func (m *MockStateManager) LoadState(name string) (*WorkflowState, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*WorkflowState), args.Error(1)
}

func (m *MockStateManager) SaveState(name string, state *WorkflowState) error {
	args := m.Called(name, state)
	return args.Error(0)
}

func (m *MockStateManager) InitState(name, description string, wfType WorkflowType) (*WorkflowState, error) {
	args := m.Called(name, description, wfType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*WorkflowState), args.Error(1)
}

func (m *MockStateManager) SavePlan(name string, plan *Plan) error {
	args := m.Called(name, plan)
	return args.Error(0)
}

func (m *MockStateManager) LoadPlan(name string) (*Plan, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Plan), args.Error(1)
}

func (m *MockStateManager) SavePlanMarkdown(name string, markdown string) error {
	args := m.Called(name, markdown)
	return args.Error(0)
}

func (m *MockStateManager) SavePhaseOutput(name string, phase Phase, data interface{}) error {
	args := m.Called(name, phase, data)
	return args.Error(0)
}

func (m *MockStateManager) LoadPhaseOutput(name string, phase Phase, target interface{}) error {
	args := m.Called(name, phase, target)
	return args.Error(0)
}

func (m *MockStateManager) ListWorkflows() ([]WorkflowInfo, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]WorkflowInfo), args.Error(1)
}

func (m *MockStateManager) DeleteWorkflow(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockStateManager) SetTimeProvider(tp TimeProvider) {
	m.Called(tp)
}

func (m *MockStateManager) SaveRawOutput(name string, phase Phase, output string) error {
	args := m.Called(name, phase, output)
	return args.Error(0)
}

func (m *MockStateManager) SavePrompt(name string, phase Phase, attempt int, prompt string) (string, error) {
	args := m.Called(name, phase, attempt, prompt)
	return args.String(0), args.Error(1)
}

// MockPromptGenerator is a mock implementation of PromptGenerator
type MockPromptGenerator struct {
	mock.Mock
}

func (m *MockPromptGenerator) GeneratePlanningPrompt(wfType WorkflowType, description string, feedback []string) (string, error) {
	args := m.Called(wfType, description, feedback)
	return args.String(0), args.Error(1)
}

func (m *MockPromptGenerator) GenerateImplementationPrompt(plan *Plan) (string, error) {
	args := m.Called(plan)
	return args.String(0), args.Error(1)
}

func (m *MockPromptGenerator) GenerateRefactoringPrompt(plan *Plan) (string, error) {
	args := m.Called(plan)
	return args.String(0), args.Error(1)
}

func (m *MockPromptGenerator) GeneratePRSplitPrompt(metrics *PRMetrics, commits []command.Commit) (string, error) {
	args := m.Called(metrics, commits)
	return args.String(0), args.Error(1)
}

func (m *MockPromptGenerator) GenerateFixCIPrompt(failures string) (string, error) {
	args := m.Called(failures)
	return args.String(0), args.Error(1)
}

func (m *MockPromptGenerator) GenerateCreatePRPrompt(ctx *PRCreationContext) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

// MockCIChecker is a mock implementation of CIChecker
type MockCIChecker struct {
	mock.Mock
}

func (m *MockCIChecker) CheckCI(ctx context.Context, prNumber int) (*CIResult, error) {
	args := m.Called(ctx, prNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*CIResult), args.Error(1)
}

func (m *MockCIChecker) WaitForCI(ctx context.Context, prNumber int, timeout time.Duration) (*CIResult, error) {
	args := m.Called(ctx, prNumber, timeout)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*CIResult), args.Error(1)
}

func (m *MockCIChecker) WaitForCIWithOptions(ctx context.Context, prNumber int, timeout time.Duration, opts CheckCIOptions) (*CIResult, error) {
	args := m.Called(ctx, prNumber, timeout, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*CIResult), args.Error(1)
}

func (m *MockCIChecker) WaitForCIWithProgress(ctx context.Context, prNumber int, timeout time.Duration, opts CheckCIOptions, onProgress CIProgressCallback) (*CIResult, error) {
	args := m.Called(ctx, prNumber, timeout, opts, onProgress)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*CIResult), args.Error(1)
}

// MockOutputParser is a mock implementation of OutputParser
type MockOutputParser struct {
	mock.Mock
}

func (m *MockOutputParser) ExtractJSON(output string) (string, error) {
	args := m.Called(output)
	return args.String(0), args.Error(1)
}

func (m *MockOutputParser) ParsePlan(jsonStr string) (*Plan, error) {
	args := m.Called(jsonStr)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Plan), args.Error(1)
}

func (m *MockOutputParser) ParseImplementationSummary(jsonStr string) (*ImplementationSummary, error) {
	args := m.Called(jsonStr)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ImplementationSummary), args.Error(1)
}

func (m *MockOutputParser) ParseRefactoringSummary(jsonStr string) (*RefactoringSummary, error) {
	args := m.Called(jsonStr)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*RefactoringSummary), args.Error(1)
}

func (m *MockOutputParser) ParsePRSplitPlan(jsonStr string) (*PRSplitPlan, error) {
	args := m.Called(jsonStr)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*PRSplitPlan), args.Error(1)
}

func (m *MockOutputParser) ParsePRSplitResult(jsonStr string) (*PRSplitResult, error) {
	args := m.Called(jsonStr)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*PRSplitResult), args.Error(1)
}

// MockWorktreeManager is a mock implementation of WorktreeManager
type MockWorktreeManager struct {
	mock.Mock
}

func (m *MockWorktreeManager) CreateWorktree(workflowName string) (string, error) {
	args := m.Called(workflowName)
	return args.String(0), args.Error(1)
}

func (m *MockWorktreeManager) CreateWorktreeFromExistingBranch(ctx context.Context, workflowName string, branchName string) (string, error) {
	args := m.Called(ctx, workflowName, branchName)
	return args.String(0), args.Error(1)
}

func (m *MockWorktreeManager) WorktreeExists(path string) bool {
	args := m.Called(path)
	return args.Bool(0)
}

func (m *MockWorktreeManager) DeleteWorktree(path string) error {
	args := m.Called(path)
	return args.Error(0)
}

// MockPRSplitManager is a mock implementation of PRSplitManager
type MockPRSplitManager struct {
	mock.Mock
}

func (m *MockPRSplitManager) ExecuteSplit(ctx context.Context, dir string, plan *PRSplitPlan, sourceBranch string, mainBranch string) (*PRSplitResult, error) {
	args := m.Called(ctx, dir, plan, sourceBranch, mainBranch)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*PRSplitResult), args.Error(1)
}

func (m *MockPRSplitManager) Rollback(ctx context.Context, dir string, result *PRSplitResult) error {
	args := m.Called(ctx, dir, result)
	return args.Error(0)
}

func TestNewOrchestrator(t *testing.T) {
	tests := []struct {
		name    string
		baseDir string
		wantErr bool
	}{
		{
			name:    "creates orchestrator with valid baseDir",
			baseDir: "/tmp/workflows",
			wantErr: false,
		},
		{
			name:    "fails with empty baseDir",
			baseDir: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewOrchestrator(tt.baseDir)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, got)
			assert.NotNil(t, got.stateManager)
			assert.NotNil(t, got.executor)
			assert.NotNil(t, got.promptGenerator)
			assert.NotNil(t, got.parser)
			assert.NotNil(t, got.config)
		})
	}
}

func TestNewOrchestratorWithConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "creates orchestrator with valid config",
			config:  DefaultConfig("/tmp/workflows"),
			wantErr: false,
		},
		{
			name:    "fails with nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "fails with empty baseDir",
			config: &Config{
				BaseDir: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewOrchestratorWithConfig(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, got)
		})
	}
}

func TestOrchestrator_executePlanning(t *testing.T) {
	tests := []struct {
		name          string
		setupMocks    func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser)
		wantErr       bool
		wantNextPhase Phase
	}{
		{
			name: "successfully generates plan",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				pg.On("GeneratePlanningPrompt", WorkflowTypeFeature, "test description", []string(nil)).Return("planning prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"test plan\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"test plan\"}", nil)
				op.On("ParsePlan", mock.Anything).Return(&Plan{Summary: "test plan"}, nil)
				sm.On("SavePlan", "test-workflow", mock.Anything).Return(nil)
				sm.On("SavePlanMarkdown", "test-workflow", mock.Anything).Return(nil)
				sm.On("SavePhaseOutput", "test-workflow", PhasePlanning, mock.Anything).Return(nil)
			},
			wantErr:       false,
			wantNextPhase: PhaseConfirmation,
		},
		{
			name: "fails when executor fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				pg.On("GeneratePlanningPrompt", WorkflowTypeFeature, "test description", []string(nil)).Return("planning prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return((*ExecuteResult)(nil), errors.New("execution failed"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test description",
				CurrentPhase: PhasePlanning,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusInProgress},
					PhaseConfirmation:   {Status: StatusPending},
					PhaseImplementation: {Status: StatusPending},
					PhaseRefactoring:    {Status: StatusPending},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.executePlanning(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantNextPhase, state.CurrentPhase)
			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executeConfirmation(t *testing.T) {
	tests := []struct {
		name          string
		confirmFunc   func(plan *Plan) (bool, string, error)
		setupMocks    func(*MockStateManager)
		wantErr       bool
		wantNextPhase Phase
	}{
		{
			name: "user approves plan",
			confirmFunc: func(plan *Plan) (bool, string, error) {
				return true, "", nil
			},
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
			},
			wantErr:       false,
			wantNextPhase: PhaseImplementation,
		},
		{
			name: "user rejects with feedback",
			confirmFunc: func(plan *Plan) (bool, string, error) {
				return false, "please add more tests", nil
			},
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
			},
			wantErr:       false,
			wantNextPhase: PhasePlanning,
		},
		{
			name: "confirmation fails",
			confirmFunc: func(plan *Plan) (bool, string, error) {
				return false, "", errors.New("user cancelled")
			},
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name: "fails when LoadPlan fails",
			confirmFunc: func(plan *Plan) (bool, string, error) {
				return true, "", nil
			},
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return((*Plan)(nil), errors.New("load plan failed"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				config:       DefaultConfig("/tmp/workflows"),
				confirmFunc:  tt.confirmFunc,
				logger:       NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseConfirmation,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusInProgress},
					PhaseImplementation: {Status: StatusPending},
					PhaseRefactoring:    {Status: StatusPending},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.executeConfirmation(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantNextPhase, state.CurrentPhase)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executeImplementation(t *testing.T) {
	tests := []struct {
		name             string
		initialWorktree  string
		setupMocks       func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker, *MockWorktreeManager)
		wantErr          bool
		wantNextPhase    Phase
		wantWorktreePath string
	}{
		{
			name:            "successfully implements plan with pre-commit passing",
			initialWorktree: "",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.WorkingDirectory == "/tmp/worktrees/test-workflow"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil)
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{Passed: true, Status: "success"}, nil)
			},
			wantErr:          false,
			wantNextPhase:    PhaseRefactoring,
			wantWorktreePath: "/tmp/worktrees/test-workflow",
		},
		{
			name:            "skips worktree creation when WorktreePath already set (resume scenario)",
			initialWorktree: "/existing/worktree/path",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				// Note: CreateWorktree should NOT be called
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.WorkingDirectory == "/existing/worktree/path"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil)
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{Passed: true, Status: "success"}, nil)
			},
			wantErr:          false,
			wantNextPhase:    PhaseRefactoring,
			wantWorktreePath: "/existing/worktree/path",
		},
		{
			name:            "fails when worktree creation fails",
			initialWorktree: "",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("", errors.New("branch already exists"))
			},
			wantErr:          true,
			wantNextPhase:    PhaseFailed,
			wantWorktreePath: "",
		},
		{
			name:            "CI check uses current branch PR automatically",
			initialWorktree: "",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil)
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)
				// CI check uses 0 for PR number (auto-detect current branch)
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{Passed: true, Status: "success"}, nil)
			},
			wantErr:          false,
			wantNextPhase:    PhaseRefactoring,
			wantWorktreePath: "/tmp/worktrees/test-workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)
			mockWM := new(MockWorktreeManager)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI, mockWM)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
				worktreeManager: mockWM,
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseImplementation,
				WorktreePath: tt.initialWorktree,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusInProgress},
					PhaseRefactoring:    {Status: StatusPending},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.executeImplementation(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantNextPhase, state.CurrentPhase)
			assert.Equal(t, tt.wantWorktreePath, state.WorktreePath)
			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
			mockWM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executeImplementation_ErrorPaths(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker, *MockWorktreeManager)
		wantErr    bool
	}{
		{
			name: "fails when LoadPlan fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return((*Plan)(nil), errors.New("load plan failed"))
			},
			wantErr: true,
		},
		{
			name: "fails when ExtractJSON fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "invalid json output",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("", errors.New("no JSON found"))
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflows/test-workflow")
				sm.On("SaveRawOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)
			},
			wantErr: true,
		},
		{
			name: "fails when ParseImplementationSummary fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"invalid\": \"schema\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"invalid\": \"schema\"}", nil)
				op.On("ParseImplementationSummary", mock.Anything).Return((*ImplementationSummary)(nil), errors.New("invalid schema"))
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflows/test-workflow")
				sm.On("SaveRawOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)
			},
			wantErr: true,
		},
		{
			name: "fails when SavePhaseOutput fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil)
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(errors.New("save failed"))
			},
			wantErr: true,
		},
		{
			name: "fails when CI check fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil)
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), errors.New("CI check timeout"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)
			mockWM := new(MockWorktreeManager)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI, mockWM)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
				worktreeManager: mockWM,
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseImplementation,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusInProgress},
					PhaseRefactoring:    {Status: StatusPending},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.executeImplementation(context.Background(), state)

			require.Error(t, err)
			assert.Equal(t, PhaseFailed, state.CurrentPhase)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executeImplementation_CIRetryLoop(t *testing.T) {
	tests := []struct {
		name          string
		setupMocks    func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker, *MockWorktreeManager)
		wantErr       bool
		wantNextPhase Phase
	}{
		{
			name: "retries when CI fails and succeeds on second attempt",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)

				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil).Once()
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil).Once()
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil).Once()
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil).Once()
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil).Once()

				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed:     false,
					Status:     "failed",
					Output:     "test failed",
					FailedJobs: []string{"test-job"},
				}, nil).Once()

				pg.On("GenerateFixCIPrompt", mock.Anything).Return("fix CI prompt", nil).Once()
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"fixed\"}\n```",
					ExitCode: 0,
				}, nil).Once()
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"fixed\"}", nil).Once()
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "fixed"}, nil).Once()
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil).Once()

				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed: true,
					Status: "success",
				}, nil).Once()
			},
			wantErr:       false,
			wantNextPhase: PhaseRefactoring,
		},
		{
			name: "fails after exceeding max fix attempts",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)

				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil).Once()
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil).Times(2)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil).Times(2)
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil).Times(2)
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil).Times(2)

				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed:     false,
					Status:     "failed",
					Output:     "test failed",
					FailedJobs: []string{"test-job"},
				}, nil).Times(2)

				pg.On("GenerateFixCIPrompt", mock.Anything).Return("fix CI prompt", nil).Once()
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name: "resumes from CI failure state",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)

				pg.On("GenerateFixCIPrompt", "CI check error: build failed").Return("fix CI prompt", nil).Once()
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"fixed\"}\n```",
					ExitCode: 0,
				}, nil).Once()
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"fixed\"}", nil).Once()
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "fixed"}, nil).Once()
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil).Once()

				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed: true,
					Status: "success",
				}, nil).Once()
			},
			wantErr:       false,
			wantNextPhase: PhaseRefactoring,
		},
		{
			name: "fails when GenerateImplementationPrompt fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("", errors.New("failed to generate prompt"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name: "fails when GenerateFixCIPrompt fails during retry",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)

				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil).Once()
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil).Once()
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil).Once()
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil).Once()
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil).Once()

				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed:     false,
					Status:     "failed",
					Output:     "test failed",
					FailedJobs: []string{"test-job"},
				}, nil).Once()

				pg.On("GenerateFixCIPrompt", mock.Anything).Return("", errors.New("failed to generate fix prompt"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)
			mockWM := new(MockWorktreeManager)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI, mockWM)

			config := DefaultConfig("/tmp/workflows")
			config.MaxFixAttempts = 2

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          config,
				logger:          NewLogger(LogLevelNormal),
				worktreeManager: mockWM,
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseImplementation,
				WorktreePath: "",
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusInProgress},
					PhaseRefactoring:    {Status: StatusPending},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			if tt.name == "resumes from CI failure state" {
				state.WorktreePath = "/existing/worktree/path"
				state.Error = &WorkflowError{
					Message:     "CI check error: build failed",
					Phase:       PhaseImplementation,
					FailureType: FailureTypeCI,
					Recoverable: true,
				}
				state.Phases[PhaseImplementation].Feedback = []string{"CI check error: build failed"}
			}

			err := o.executeImplementation(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantNextPhase, state.CurrentPhase)
			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
			mockCI.AssertExpectations(t)
			mockWM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executeRefactoring_ErrorPaths(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker)
		wantErr    bool
	}{
		{
			name: "fails when LoadPlan fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return((*Plan)(nil), errors.New("load plan failed"))
			},
			wantErr: true,
		},
		{
			name: "fails when ExtractJSON fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "invalid json output",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("", errors.New("no JSON found"))
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflows/test-workflow")
				sm.On("SaveRawOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil)
			},
			wantErr: true,
		},
		{
			name: "fails when ParseRefactoringSummary fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"invalid\": \"schema\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"invalid\": \"schema\"}", nil)
				op.On("ParseRefactoringSummary", mock.Anything).Return((*RefactoringSummary)(nil), errors.New("invalid schema"))
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflows/test-workflow")
				sm.On("SaveRawOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil)
			},
			wantErr: true,
		},
		{
			name: "fails when SavePhaseOutput fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"refactored\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"refactored\"}", nil)
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{Summary: "refactored"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(errors.New("save failed"))
			},
			wantErr: true,
		},
		{
			name: "fails when CI check fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"refactored\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"refactored\"}", nil)
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{Summary: "refactored"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), errors.New("CI check timeout"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseRefactoring,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusCompleted},
					PhaseRefactoring:    {Status: StatusInProgress},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.executeRefactoring(context.Background(), state)

			require.Error(t, err)
			assert.Equal(t, PhaseFailed, state.CurrentPhase)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executeRefactoring_CIRetryLoop(t *testing.T) {
	tests := []struct {
		name          string
		setupMocks    func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker)
		wantErr       bool
		wantNextPhase Phase
	}{
		// Note: Tests where CI passes after refactoring are skipped because they call getPRMetrics
		// which requires a git repository. See TestOrchestrator_executePhase for the happy path test.
		{
			name: "fails after exceeding max fix attempts",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)

				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil).Once()
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"refactored\"}\n```",
					ExitCode: 0,
				}, nil).Times(2)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"refactored\"}", nil).Times(2)
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{Summary: "refactored"}, nil).Times(2)
				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil).Times(2)

				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed:     false,
					Status:     "failed",
					Output:     "test failed",
					FailedJobs: []string{"test-job"},
				}, nil).Times(2)

				pg.On("GenerateFixCIPrompt", mock.Anything).Return("fix CI prompt", nil).Once()
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name: "resumes from CI failure state and exceeds max attempts",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)

				// When resuming from CI failure, startAttempt=2, so with MaxFixAttempts=2, it only runs once
				pg.On("GenerateFixCIPrompt", "CI check error: build failed").Return("fix CI prompt", nil).Once()
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"fixed\"}\n```",
					ExitCode: 0,
				}, nil).Once()
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"fixed\"}", nil).Once()
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{Summary: "fixed"}, nil).Once()
				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil).Once()

				// CI still fails, and since this is already attempt 2 (max), it should fail
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed:     false,
					Status:     "failed",
					Output:     "still failing",
					FailedJobs: []string{"test-job"},
				}, nil).Once()
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name: "fails when GenerateRefactoringPrompt fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("", errors.New("failed to generate prompt"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name: "fails when GenerateFixCIPrompt fails during retry",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)

				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil).Once()
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"refactored\"}\n```",
					ExitCode: 0,
				}, nil).Once()
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"refactored\"}", nil).Once()
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{Summary: "refactored"}, nil).Once()
				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil).Once()

				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed:     false,
					Status:     "failed",
					Output:     "test failed",
					FailedJobs: []string{"test-job"},
				}, nil).Once()

				pg.On("GenerateFixCIPrompt", mock.Anything).Return("", errors.New("failed to generate fix prompt"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI)

			config := DefaultConfig("/tmp/workflows")
			config.MaxFixAttempts = 2

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          config,
				logger:          NewLogger(LogLevelNormal),
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			workDir, _ := os.Getwd()
			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseRefactoring,
				WorktreePath: workDir,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusCompleted},
					PhaseRefactoring:    {Status: StatusInProgress},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			if tt.name == "resumes from CI failure state and exceeds max attempts" {
				state.Error = &WorkflowError{
					Message:     "CI check error: build failed",
					Phase:       PhaseRefactoring,
					FailureType: FailureTypeCI,
					Recoverable: true,
				}
				state.Phases[PhaseRefactoring].Feedback = []string{"CI check error: build failed"}
			}

			err := o.executeRefactoring(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantNextPhase, state.CurrentPhase)
			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
			mockCI.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executePhase(t *testing.T) {
	tests := []struct {
		name       string
		phase      Phase
		setupMocks func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker, *MockWorktreeManager, *MockGitRunner)
		wantErr    bool
	}{
		{
			name:  "executes PhasePlanning",
			phase: PhasePlanning,
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager, git *MockGitRunner) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				pg.On("GeneratePlanningPrompt", WorkflowTypeFeature, "test description", []string(nil)).Return("planning prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"test plan\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"test plan\"}", nil)
				op.On("ParsePlan", mock.Anything).Return(&Plan{Summary: "test plan"}, nil)
				sm.On("SavePlan", "test-workflow", mock.Anything).Return(nil)
				sm.On("SavePlanMarkdown", "test-workflow", mock.Anything).Return(nil)
				sm.On("SavePhaseOutput", "test-workflow", PhasePlanning, mock.Anything).Return(nil)
			},
			wantErr: false,
		},
		{
			name:  "executes PhaseConfirmation",
			phase: PhaseConfirmation,
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager, git *MockGitRunner) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
			},
			wantErr: false,
		},
		{
			name:  "executes PhaseImplementation",
			phase: PhaseImplementation,
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager, git *MockGitRunner) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				wm.On("CreateWorktree", "test-workflow").Return("/tmp/worktrees/test-workflow", nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"implemented\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"implemented\"}", nil)
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{Summary: "implemented"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{Passed: true, Status: "success"}, nil)
			},
			wantErr: false,
		},
		// Note: PhaseRefactoring test is in TestOrchestrator_executeRefactoring since it requires git repo setup
		{
			name:  "executes PhasePRSplit",
			phase: PhasePRSplit,
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wm *MockWorktreeManager, git *MockGitRunner) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				git.On("GetCurrentBranch", mock.Anything, mock.Anything).Return("feature-branch", nil)
				git.On("GetCommits", mock.Anything, mock.Anything, "main").Return([]command.Commit{
					{Hash: "abc123", Subject: "feat: add feature"},
				}, nil)
				pg.On("GeneratePRSplitPrompt", mock.Anything, mock.Anything).Return("pr split prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"strategy\":\"commits\",\"parentTitle\":\"Parent\",\"parentDescription\":\"Desc\",\"summary\":\"Split\",\"childPRs\":[]}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"strategy\":\"commits\",\"parentTitle\":\"Parent\",\"parentDescription\":\"Desc\",\"summary\":\"Split\",\"childPRs\":[]}", nil)
				op.On("ParsePRSplitPlan", mock.Anything).Return(&PRSplitPlan{Strategy: SplitByCommits, ParentTitle: "Parent", ParentDesc: "Desc", Summary: "Split", ChildPRs: []ChildPRPlan{}}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhasePRSplit, mock.Anything).Return(nil)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)
			mockWM := new(MockWorktreeManager)
			mockSplitMgr := new(MockPRSplitManager)
			mockGit := new(MockGitRunner)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI, mockWM, mockGit)

			if tt.phase == PhasePRSplit {
				mockSplitMgr.On("ExecuteSplit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&PRSplitResult{
					ParentPR: PRInfo{Number: 1, Title: "Parent"},
					ChildPRs: []PRInfo{},
					Summary:  "Split",
				}, nil)
			}

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
				worktreeManager: mockWM,
				gitRunner:       mockGit,
				splitManager:    mockSplitMgr,
				confirmFunc: func(plan *Plan) (bool, string, error) {
					return true, "", nil
				},
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test description",
				CurrentPhase: tt.phase,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusCompleted},
					PhaseRefactoring:    {Status: StatusCompleted},
					PhasePRSplit:        {Status: StatusInProgress, Metrics: &PRMetrics{FilesChanged: 10, LinesChanged: 100}},
				},
			}

			err := o.executePhase(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executePhase_InvalidPhase(t *testing.T) {
	mockSM := new(MockStateManager)
	mockSM.On("SaveState", "test-workflow", mock.Anything).Return(nil)

	o := &Orchestrator{
		stateManager: mockSM,
		config:       DefaultConfig("/tmp/workflows"),
		logger:       NewLogger(LogLevelNormal),
	}

	state := &WorkflowState{
		Name:         "test-workflow",
		CurrentPhase: "INVALID_PHASE",
		Phases: map[Phase]*PhaseState{
			"INVALID_PHASE": {Status: StatusInProgress},
		},
	}

	err := o.executePhase(context.Background(), state)

	require.Error(t, err)
	assert.Equal(t, PhaseFailed, state.CurrentPhase)
	mockSM.AssertExpectations(t)
}

func TestOrchestrator_Start(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*MockStateManager, *command.MockGitRunner, *command.MockGhRunner)
		updatePR    *int
		wantErr     bool
		errContains string
	}{
		{
			name: "fails when InitState fails",
			setupMocks: func(sm *MockStateManager, gr *command.MockGitRunner, ghr *command.MockGhRunner) {
				sm.On("WorkflowExists", "test-workflow").Return(false)
				sm.On("InitState", "test-workflow", "test description", WorkflowTypeFeature).Return((*WorkflowState)(nil), errors.New("init failed"))
			},
			updatePR: nil,
			wantErr:  true,
		},
		{
			name: "deletes and restarts failed workflow",
			setupMocks: func(sm *MockStateManager, gr *command.MockGitRunner, ghr *command.MockGhRunner) {
				sm.On("WorkflowExists", "test-workflow").Return(true)
				sm.On("LoadState", "test-workflow").Return(&WorkflowState{
					Name:         "test-workflow",
					CurrentPhase: PhaseFailed,
				}, nil)
				sm.On("DeleteWorkflow", "test-workflow").Return(nil)
				sm.On("InitState", "test-workflow", "test description", WorkflowTypeFeature).Return((*WorkflowState)(nil), errors.New("init failed"))
			},
			updatePR: nil,
			wantErr:  true,
		},
		{
			name: "fails when workflow exists and not failed",
			setupMocks: func(sm *MockStateManager, gr *command.MockGitRunner, ghr *command.MockGhRunner) {
				sm.On("WorkflowExists", "test-workflow").Return(true)
				sm.On("LoadState", "test-workflow").Return(&WorkflowState{
					Name:         "test-workflow",
					CurrentPhase: PhaseImplementation,
				}, nil)
				sm.On("InitState", "test-workflow", "test description", WorkflowTypeFeature).Return((*WorkflowState)(nil), ErrWorkflowExists)
			},
			updatePR: nil,
			wantErr:  true,
		},
		{
			name: "fails when deleting failed workflow fails",
			setupMocks: func(sm *MockStateManager, gr *command.MockGitRunner, ghr *command.MockGhRunner) {
				sm.On("WorkflowExists", "test-workflow").Return(true)
				sm.On("LoadState", "test-workflow").Return(&WorkflowState{
					Name:         "test-workflow",
					CurrentPhase: PhaseFailed,
				}, nil)
				sm.On("DeleteWorkflow", "test-workflow").Return(errors.New("delete failed"))
			},
			updatePR: nil,
			wantErr:  true,
		},
		{
			name: "continues when LoadState fails for existing workflow",
			setupMocks: func(sm *MockStateManager, gr *command.MockGitRunner, ghr *command.MockGhRunner) {
				sm.On("WorkflowExists", "test-workflow").Return(true)
				sm.On("LoadState", "test-workflow").Return((*WorkflowState)(nil), errors.New("load failed"))
				sm.On("InitState", "test-workflow", "test description", WorkflowTypeFeature).Return((*WorkflowState)(nil), ErrWorkflowExists)
			},
			updatePR: nil,
			wantErr:  true,
		},
		{
			name: "fails when updatePR validation fails",
			setupMocks: func(sm *MockStateManager, gr *command.MockGitRunner, ghr *command.MockGhRunner) {
				ghr.EXPECT().
					PRView(gomock.Any(), "/tmp/workflows", "number,state,headRefName,baseRefName,mergeable", ".").
					Return("", errors.New("gh command failed"))
			},
			updatePR:    intPtr(123),
			wantErr:     true,
			errContains: "failed to validate PR #123 for update",
		},
		{
			name: "fails when PR is not open for update",
			setupMocks: func(sm *MockStateManager, gr *command.MockGitRunner, ghr *command.MockGhRunner) {
				prData := map[string]interface{}{
					"number":      float64(123),
					"state":       "CLOSED",
					"headRefName": "feature-branch",
					"baseRefName": "main",
					"mergeable":   "MERGEABLE",
				}
				jsonBytes, _ := json.Marshal(prData)
				ghr.EXPECT().
					PRView(gomock.Any(), "/tmp/workflows", "number,state,headRefName,baseRefName,mergeable", ".").
					Return(string(jsonBytes), nil)
			},
			updatePR:    intPtr(123),
			wantErr:     true,
			errContains: "PR #123 is CLOSED, cannot update",
		},
		{
			name: "sets updatePR fields when updatePR is valid",
			setupMocks: func(sm *MockStateManager, gr *command.MockGitRunner, ghr *command.MockGhRunner) {
				prData := map[string]interface{}{
					"number":      float64(123),
					"state":       "OPEN",
					"headRefName": "feature-branch",
					"baseRefName": "main",
					"mergeable":   "MERGEABLE",
				}
				jsonBytes, _ := json.Marshal(prData)

				// First validation call
				ghr.EXPECT().
					PRView(gomock.Any(), "/tmp/workflows", "number,state,headRefName,baseRefName,mergeable", ".").
					Return(string(jsonBytes), nil)

				sm.On("WorkflowExists", "test-workflow").Return(false)
				sm.On("InitState", "test-workflow", "test description", WorkflowTypeFeature).Return(&WorkflowState{
					Name:         "test-workflow",
					CurrentPhase: PhasePlanning,
					Phases:       map[Phase]*PhaseState{},
				}, nil)

				// Second validation call
				ghr.EXPECT().
					PRView(gomock.Any(), "/tmp/workflows", "number,state,headRefName,baseRefName,mergeable", ".").
					Return(string(jsonBytes), nil)

				// SaveState called with updatePR and updatePRBranch set
				sm.On("SaveState", "test-workflow", mock.MatchedBy(func(state *WorkflowState) bool {
					return state.UpdatePR != nil &&
						*state.UpdatePR == 123 &&
						state.UpdatePRBranch == "feature-branch"
				})).Return(errors.New("stop execution"))
			},
			updatePR:    intPtr(123),
			wantErr:     true,
			errContains: "stop execution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockSM := new(MockStateManager)
			mockGitRunner := command.NewMockGitRunner(ctrl)
			mockGhRunner := command.NewMockGhRunner(ctrl)

			tt.setupMocks(mockSM, mockGitRunner, mockGhRunner)

			o := &Orchestrator{
				stateManager: mockSM,
				config:       DefaultConfig("/tmp/workflows"),
				logger:       NewLogger(LogLevelNormal),
				gitRunner:    mockGitRunner,
				ghRunner:     mockGhRunner,
			}

			err := o.Start(context.Background(), "test-workflow", "test description", WorkflowTypeFeature, tt.updatePR)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			mockSM.AssertExpectations(t)
		})
	}
}

// Helper function to create int pointers
func intPtr(i int) *int {
	return &i
}

func TestOrchestrator_transitionPhase(t *testing.T) {
	tests := []struct {
		name         string
		currentPhase Phase
		nextPhase    Phase
		setupMocks   func(*MockStateManager)
		wantErr      bool
	}{
		{
			name:         "transitions from planning to confirmation",
			currentPhase: PhasePlanning,
			nextPhase:    PhaseConfirmation,
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
			},
			wantErr: false,
		},
		{
			name:         "transitions to completed",
			currentPhase: PhaseRefactoring,
			nextPhase:    PhaseCompleted,
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
			},
			wantErr: false,
		},
		{
			name:         "returns error when SaveState fails",
			currentPhase: PhasePlanning,
			nextPhase:    PhaseConfirmation,
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(errors.New("save failed"))
			},
			wantErr: true,
		},
		{
			name:         "transitions to failed",
			currentPhase: PhasePlanning,
			nextPhase:    PhaseFailed,
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				config:       DefaultConfig("/tmp/workflows"),
				logger:       NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: tt.currentPhase,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusInProgress},
					PhaseConfirmation:   {Status: StatusPending},
					PhaseImplementation: {Status: StatusPending},
					PhaseRefactoring:    {Status: StatusPending},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.transitionPhase(state, tt.nextPhase)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.nextPhase, state.CurrentPhase)
			assert.Equal(t, StatusCompleted, state.Phases[tt.currentPhase].Status)

			if tt.nextPhase != PhaseCompleted && tt.nextPhase != PhaseFailed {
				assert.Equal(t, StatusInProgress, state.Phases[tt.nextPhase].Status)
			}

			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_Resume(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager)
		wantErr    bool
		errMsg     string
	}{
		{
			name: "cannot resume completed workflow",
			setupMocks: func(sm *MockStateManager) {
				sm.On("LoadState", "test-workflow").Return(&WorkflowState{
					Name:         "test-workflow",
					CurrentPhase: PhaseCompleted,
				}, nil)
			},
			wantErr: true,
			errMsg:  "already completed",
		},
		{
			name: "cannot resume non-recoverable error",
			setupMocks: func(sm *MockStateManager) {
				sm.On("LoadState", "test-workflow").Return(&WorkflowState{
					Name:         "test-workflow",
					CurrentPhase: PhaseFailed,
					Error: &WorkflowError{
						Message:     "non-recoverable error",
						Recoverable: false,
					},
				}, nil)
			},
			wantErr: true,
			errMsg:  "non-recoverable error state",
		},
		{
			name: "fails when LoadState fails",
			setupMocks: func(sm *MockStateManager) {
				sm.On("LoadState", "test-workflow").Return((*WorkflowState)(nil), errors.New("load failed"))
			},
			wantErr: true,
			errMsg:  "failed to load workflow state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				config:       DefaultConfig("/tmp/workflows"),
				logger:       NewLogger(LogLevelNormal),
			}

			err := o.Resume(context.Background(), "test-workflow")

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			require.NoError(t, err)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_Resume_RestoresFailedPhase(t *testing.T) {
	tests := []struct {
		name                string
		initialState        *WorkflowState
		expectedPhase       Phase
		expectedPhaseStatus PhaseStatus
	}{
		{
			name: "restores phase from error.Phase when error exists",
			initialState: &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseFailed,
				Phases: map[Phase]*PhaseState{
					PhaseImplementation: {Status: StatusFailed},
					PhasePlanning:       {Status: StatusCompleted},
				},
				Error: &WorkflowError{
					Message:     "parse error",
					Phase:       PhaseImplementation,
					Recoverable: true,
				},
			},
			expectedPhase:       PhaseImplementation,
			expectedPhaseStatus: StatusInProgress,
		},
		{
			name: "finds failed phase when error is nil",
			initialState: &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseFailed,
				Phases: map[Phase]*PhaseState{
					PhaseImplementation: {Status: StatusFailed},
					PhasePlanning:       {Status: StatusCompleted},
				},
			},
			expectedPhase:       PhaseImplementation,
			expectedPhaseStatus: StatusInProgress,
		},
		{
			name: "finds in_progress phase when error is nil",
			initialState: &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseFailed,
				Phases: map[Phase]*PhaseState{
					PhaseRefactoring: {Status: StatusInProgress},
					PhasePlanning:    {Status: StatusCompleted},
				},
			},
			expectedPhase:       PhaseRefactoring,
			expectedPhaseStatus: StatusInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockSM.On("LoadState", "test-workflow").Return(tt.initialState, nil)

			// Capture the saved state to verify
			var savedState *WorkflowState
			mockSM.On("SaveState", "test-workflow", mock.Anything).Run(func(args mock.Arguments) {
				savedState = args.Get(1).(*WorkflowState)
			}).Return(errors.New("stop execution for test"))

			o := &Orchestrator{
				stateManager: mockSM,
				config:       DefaultConfig("/tmp/workflows"),
				logger:       NewLogger(LogLevelNormal),
			}

			// Resume will fail because SaveState returns error, but we verify state was correctly set
			err := o.Resume(context.Background(), "test-workflow")
			require.Error(t, err)

			// Verify the state was correctly modified before save
			assert.Equal(t, tt.expectedPhase, savedState.CurrentPhase)
			assert.Nil(t, savedState.Error)
			assert.Equal(t, tt.expectedPhaseStatus, savedState.Phases[tt.expectedPhase].Status)

			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_Resume_PreservesCIFailure(t *testing.T) {
	tests := []struct {
		name                string
		initialState        *WorkflowState
		expectedPhase       Phase
		expectedErrorNil    bool
		expectedFailureType FailureType
	}{
		{
			name: "preserves CI failure error for implementation phase",
			initialState: &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseFailed,
				Phases: map[Phase]*PhaseState{
					PhaseImplementation: {
						Status:   StatusFailed,
						Feedback: []string{"CI check error: CI check timeout after 30m0s"},
					},
					PhasePlanning: {Status: StatusCompleted},
				},
				Error: &WorkflowError{
					Message:     "failed to check CI: CI check timeout after 30m0s",
					Phase:       PhaseImplementation,
					Recoverable: true,
					FailureType: FailureTypeCI,
				},
			},
			expectedPhase:       PhaseImplementation,
			expectedErrorNil:    false,
			expectedFailureType: FailureTypeCI,
		},
		{
			name: "clears non-CI execution failure error",
			initialState: &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseFailed,
				Phases: map[Phase]*PhaseState{
					PhaseImplementation: {Status: StatusFailed},
					PhasePlanning:       {Status: StatusCompleted},
				},
				Error: &WorkflowError{
					Message:     "failed to execute implementation: timeout",
					Phase:       PhaseImplementation,
					Recoverable: true,
					FailureType: FailureTypeExecution,
				},
			},
			expectedPhase:    PhaseImplementation,
			expectedErrorNil: true,
		},
		{
			name: "preserves CI failure error for refactoring phase",
			initialState: &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseFailed,
				Phases: map[Phase]*PhaseState{
					PhaseRefactoring: {
						Status:   StatusFailed,
						Feedback: []string{"CI check error: CI check timeout after 30m0s"},
					},
					PhaseImplementation: {Status: StatusCompleted},
					PhasePlanning:       {Status: StatusCompleted},
				},
				Error: &WorkflowError{
					Message:     "failed to check CI: CI check timeout after 30m0s",
					Phase:       PhaseRefactoring,
					Recoverable: true,
					FailureType: FailureTypeCI,
				},
			},
			expectedPhase:       PhaseRefactoring,
			expectedErrorNil:    false,
			expectedFailureType: FailureTypeCI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockSM.On("LoadState", "test-workflow").Return(tt.initialState, nil)

			// Capture the saved state to verify
			var savedState *WorkflowState
			mockSM.On("SaveState", "test-workflow", mock.Anything).Run(func(args mock.Arguments) {
				savedState = args.Get(1).(*WorkflowState)
			}).Return(errors.New("stop execution for test"))

			o := &Orchestrator{
				stateManager: mockSM,
				config:       DefaultConfig("/tmp/workflows"),
				logger:       NewLogger(LogLevelNormal),
			}

			// Resume will fail because SaveState returns error, but we verify state was correctly set
			err := o.Resume(context.Background(), "test-workflow")
			require.Error(t, err)

			// Verify the state was correctly modified before save
			assert.Equal(t, tt.expectedPhase, savedState.CurrentPhase)
			if tt.expectedErrorNil {
				assert.Nil(t, savedState.Error)
			} else {
				assert.NotNil(t, savedState.Error)
				assert.Equal(t, tt.expectedFailureType, savedState.Error.FailureType)
			}

			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_List(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager)
		want       []WorkflowInfo
		wantErr    bool
	}{
		{
			name: "successfully lists workflows",
			setupMocks: func(sm *MockStateManager) {
				workflows := []WorkflowInfo{
					{
						Name:         "workflow-1",
						Type:         WorkflowTypeFeature,
						CurrentPhase: PhasePlanning,
						Status:       "in_progress",
					},
				}
				sm.On("ListWorkflows").Return(workflows, nil)
			},
			want: []WorkflowInfo{
				{
					Name:         "workflow-1",
					Type:         WorkflowTypeFeature,
					CurrentPhase: PhasePlanning,
					Status:       "in_progress",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				logger:       NewLogger(LogLevelNormal),
			}

			got, err := o.List()

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_Clean(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager)
		want       []string
		wantErr    bool
	}{
		{
			name: "successfully cleans completed workflows",
			setupMocks: func(sm *MockStateManager) {
				workflows := []WorkflowInfo{
					{Name: "workflow-1", Status: "completed"},
					{Name: "workflow-2", Status: "in_progress"},
					{Name: "workflow-3", Status: "completed"},
				}
				sm.On("ListWorkflows").Return(workflows, nil)
				sm.On("DeleteWorkflow", "workflow-1").Return(nil)
				sm.On("DeleteWorkflow", "workflow-3").Return(nil)
			},
			want:    []string{"workflow-1", "workflow-3"},
			wantErr: false,
		},
		{
			name: "returns error when ListWorkflows fails",
			setupMocks: func(sm *MockStateManager) {
				sm.On("ListWorkflows").Return([]WorkflowInfo(nil), errors.New("list failed"))
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "continues on delete error and deletes other workflows",
			setupMocks: func(sm *MockStateManager) {
				workflows := []WorkflowInfo{
					{Name: "workflow-1", Status: "completed"},
					{Name: "workflow-2", Status: "completed"},
				}
				sm.On("ListWorkflows").Return(workflows, nil)
				sm.On("DeleteWorkflow", "workflow-1").Return(errors.New("delete failed"))
				sm.On("DeleteWorkflow", "workflow-2").Return(nil)
			},
			want:    []string{"workflow-2"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				logger:       NewLogger(LogLevelNormal),
			}

			got, err := o.Clean()

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestIsRecoverableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error is not recoverable",
			err:  nil,
			want: false,
		},
		{
			name: "timeout error is recoverable",
			err:  errors.New("operation timeout"),
			want: true,
		},
		{
			name: "connection timeout is recoverable",
			err:  errors.New("connection timeout after 30s"),
			want: true,
		},
		{
			name: "claude execution timeout is recoverable",
			err:  errors.New("claude execution timeout"),
			want: true,
		},
		{
			name: "claude execution failed is recoverable",
			err:  errors.New("claude execution failed"),
			want: true,
		},
		{
			name: "claude execution failed with exit code is recoverable",
			err:  errors.New("claude execution failed with exit code 1"),
			want: true,
		},
		{
			name: "failed to parse JSON is recoverable",
			err:  errors.New("failed to parse JSON"),
			want: true,
		},
		{
			name: "failed to parse YAML is recoverable",
			err:  errors.New("failed to parse YAML"),
			want: true,
		},
		{
			name: "failed to parse response is recoverable",
			err:  errors.New("failed to parse response"),
			want: true,
		},
		{
			name: "invalid workflow name is not recoverable",
			err:  errors.New("invalid workflow name"),
			want: false,
		},
		{
			name: "invalid phase is not recoverable",
			err:  errors.New("invalid phase transition"),
			want: false,
		},
		{
			name: "invalid configuration is not recoverable",
			err:  errors.New("invalid configuration"),
			want: false,
		},
		{
			name: "invalid input is not recoverable",
			err:  errors.New("invalid input parameter"),
			want: false,
		},
		{
			name: "generic error is recoverable by default",
			err:  errors.New("something went wrong"),
			want: true,
		},
		{
			name: "network error is recoverable by default",
			err:  errors.New("network connection lost"),
			want: true,
		},
		{
			name: "temporary error is recoverable by default",
			err:  errors.New("temporary failure, please retry"),
			want: true,
		},
		{
			name: "timeout at start is recoverable",
			err:  errors.New("timeout at connection start"),
			want: true,
		},
		{
			name: "timeout at end is recoverable",
			err:  errors.New("operation ended with timeout"),
			want: true,
		},
		{
			name: "timeout in middle is recoverable",
			err:  errors.New("operation timeout during execution"),
			want: true,
		},
		{
			name: "context deadline exceeded is recoverable",
			err:  errors.New("context deadline exceeded timeout"),
			want: true,
		},
		{
			name: "invalid at start of message is not recoverable",
			err:  errors.New("invalid request parameters"),
			want: false,
		},
		{
			name: "invalid in middle of message is not recoverable",
			err:  errors.New("request has invalid data"),
			want: false,
		},
		{
			name: "invalid workflow name with context is not recoverable",
			err:  errors.New("invalid workflow name: cannot contain spaces"),
			want: false,
		},
		{
			name: "parse error in JSON is recoverable",
			err:  errors.New("failed to parse JSON response from API"),
			want: true,
		},
		{
			name: "parse error with details is recoverable",
			err:  errors.New("failed to parse output: unexpected character at position 42"),
			want: true,
		},
		{
			name: "claude execution failed with details is recoverable",
			err:  errors.New("claude execution failed: connection reset by peer"),
			want: true,
		},
		{
			name: "claude execution failed with status code is recoverable",
			err:  errors.New("claude execution failed: HTTP 503 Service Unavailable"),
			want: true,
		},
		{
			name: "database error is recoverable by default",
			err:  errors.New("database connection failed"),
			want: true,
		},
		{
			name: "file not found is recoverable by default",
			err:  errors.New("file not found: config.yaml"),
			want: true,
		},
		{
			name: "permission denied is recoverable by default",
			err:  errors.New("permission denied accessing file"),
			want: true,
		},
		{
			name: "invalid type is not recoverable",
			err:  errors.New("invalid type provided"),
			want: false,
		},
		{
			name: "invalid format is not recoverable",
			err:  errors.New("invalid format specified"),
			want: false,
		},
		{
			name: "invalid state is not recoverable",
			err:  errors.New("invalid state transition requested"),
			want: false,
		},
		{
			name: "disk full error is recoverable by default",
			err:  errors.New("no space left on device"),
			want: true,
		},
		{
			name: "out of memory error is recoverable by default",
			err:  errors.New("out of memory"),
			want: true,
		},
		{
			name: "rate limit error is recoverable by default",
			err:  errors.New("rate limit exceeded, retry after 60s"),
			want: true,
		},
		{
			name: "service unavailable is recoverable by default",
			err:  errors.New("service temporarily unavailable"),
			want: true,
		},
		{
			name: "error with timeout word capitalized is recoverable",
			err:  errors.New("Request Timeout occurred"),
			want: true,
		},
		{
			name: "error with parse word capitalized is recoverable",
			err:  errors.New("Failed to Parse the response"),
			want: true,
		},
		{
			name: "error with Invalid word capitalized is recoverable since check is case-sensitive",
			err:  errors.New("Invalid configuration detected"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRecoverableError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	baseDir := "/tmp/workflows"
	config := DefaultConfig(baseDir)

	assert.Equal(t, baseDir, config.BaseDir)
	assert.Equal(t, false, config.SplitPR)
	assert.Equal(t, "claude", config.ClaudePath)
	assert.Equal(t, 1*time.Hour, config.Timeouts.Planning)
	assert.Equal(t, 6*time.Hour, config.Timeouts.Implementation)
	assert.Equal(t, 6*time.Hour, config.Timeouts.Refactoring)
	assert.Equal(t, 1*time.Hour, config.Timeouts.PRSplit)
}

func TestOrchestrator_SetConfirmFunc(t *testing.T) {
	o := &Orchestrator{
		logger: NewLogger(LogLevelNormal),
	}
	customFunc := func(plan *Plan) (bool, string, error) {
		return true, "", nil
	}

	o.SetConfirmFunc(customFunc)
	assert.NotNil(t, o.confirmFunc)
}

func TestParseDiffStat(t *testing.T) {
	tests := []struct {
		name       string
		diffOutput string
		want       *PRMetrics
		wantErr    bool
	}{
		{
			name: "single file changed",
			diffOutput: ` main.go | 10 ++++++++++
 1 file changed, 10 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  10,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"main.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "multiple files changed",
			diffOutput: ` file1.go | 10 ++++++++++
 file2.go | 5 +++++
 file3.go | 3 +++
 3 files changed, 18 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  18,
				FilesChanged:  3,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go", "file2.go", "file3.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name:       "no changes",
			diffOutput: "",
			want: &PRMetrics{
				FilesAdded:    []string{},
				FilesModified: []string{},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "new file added",
			diffOutput: ` newfile.go (new) | 20 ++++++++++++++++++++
 1 file changed, 20 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  20,
				FilesChanged:  1,
				FilesAdded:    []string{"newfile.go"},
				FilesModified: []string{},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "file deleted",
			diffOutput: ` oldfile.go (gone) | 50 --------------------------------------------------
 1 file changed, 50 deletions(-)`,
			want: &PRMetrics{
				LinesChanged:  50,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{},
				FilesDeleted:  []string{"oldfile.go"},
			},
			wantErr: false,
		},
		{
			name: "mixed changes with additions modifications and deletions",
			diffOutput: ` newfile.go (new) | 20 ++++++++++++++++++++
 existing.go | 10 ++++++++++
 oldfile.go (gone) | 15 ---------------
 3 files changed, 30 insertions(+), 15 deletions(-)`,
			want: &PRMetrics{
				LinesChanged:  30,
				FilesChanged:  3,
				FilesAdded:    []string{"newfile.go"},
				FilesModified: []string{"existing.go"},
				FilesDeleted:  []string{"oldfile.go"},
			},
			wantErr: false,
		},
		{
			name: "files with paths",
			diffOutput: ` internal/workflow/orchestrator.go | 25 +++++++++++++++++++
 cmd/main.go | 5 +++++
 2 files changed, 30 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  30,
				FilesChanged:  2,
				FilesAdded:    []string{},
				FilesModified: []string{"internal/workflow/orchestrator.go", "cmd/main.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "binary files",
			diffOutput: ` image.png | Bin 0 -> 1024 bytes
 data.bin  | Bin 2048 -> 4096 bytes
 2 files changed`,
			want: &PRMetrics{
				LinesChanged:  0,
				FilesChanged:  2,
				FilesAdded:    []string{},
				FilesModified: []string{"image.png", "data.bin"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "renamed files",
			diffOutput: ` old.go => new.go | 5 +++++
 1 file changed, 5 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  5,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"old.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "large number of changes",
			diffOutput: ` file1.go | 100 ++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++
 file2.go | 50 +++++++++++++++++++++++++++++
 2 files changed, 150 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  150,
				FilesChanged:  2,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go", "file2.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "deletions only",
			diffOutput: ` file1.go | 10 ----------
 file2.go | 5 -----
 2 files changed, 15 deletions(-)`,
			want: &PRMetrics{
				LinesChanged:  15,
				FilesChanged:  2,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go", "file2.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "insertions and deletions combined",
			diffOutput: ` file1.go | 25 +++++++++++--------------
 1 file changed, 12 insertions(+), 13 deletions(-)`,
			want: &PRMetrics{
				LinesChanged:  12,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "whitespace-only line",
			diffOutput: ` file1.go | 10 ++++++++++

 1 file changed, 10 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  10,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name:       "only summary line no files",
			diffOutput: ` 0 files changed`,
			want: &PRMetrics{
				LinesChanged:  0,
				FilesChanged:  0,
				FilesAdded:    []string{},
				FilesModified: []string{},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "file with very long path",
			diffOutput: ` internal/very/deep/nested/path/to/some/file/in/the/project/structure/file.go | 5 +++++
 1 file changed, 5 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  5,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"internal/very/deep/nested/path/to/some/file/in/the/project/structure/file.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "file with dots in name",
			diffOutput: ` test.config.json | 3 +++
 1 file changed, 3 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  3,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"test.config.json"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "file with numbers in name",
			diffOutput: ` migration_001_initial.sql | 20 ++++++++++++++++++++
 1 file changed, 20 insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  20,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"migration_001_initial.sql"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "summary with only files changed field",
			diffOutput: ` file1.go | 10 ++++++++++
 1 file changed`,
			want: &PRMetrics{
				LinesChanged:  0,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "mixed new deleted and modified in complex scenario",
			diffOutput: ` new1.go (new) | 30 ++++++++++++++++++++++++++++++
 new2.go (new) | 15 +++++++++++++++
 existing1.go | 10 ++++++++++
 existing2.go | 5 -----
 old1.go (gone) | 25 -------------------------
 old2.go (gone) | 10 ----------
 6 files changed, 55 insertions(+), 40 deletions(-)`,
			want: &PRMetrics{
				LinesChanged:  55,
				FilesChanged:  6,
				FilesAdded:    []string{"new1.go", "new2.go"},
				FilesModified: []string{"existing1.go", "existing2.go"},
				FilesDeleted:  []string{"old1.go", "old2.go"},
			},
			wantErr: false,
		},
		{
			name: "trailing whitespace in diff output",
			diffOutput: ` file1.go | 10 ++++++++++
 1 file changed, 10 insertions(+)   `,
			want: &PRMetrics{
				LinesChanged:  10,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name:       "single line output with no newline",
			diffOutput: ` 0 files changed`,
			want: &PRMetrics{
				LinesChanged:  0,
				FilesChanged:  0,
				FilesAdded:    []string{},
				FilesModified: []string{},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "summary line with non-numeric parts",
			diffOutput: ` file1.go | 10 ++++++++++
 abc files changed, def insertions(+)`,
			want: &PRMetrics{
				LinesChanged:  0,
				FilesChanged:  0,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
		{
			name: "summary with only 2 parts",
			diffOutput: ` file1.go | 10 ++++++++++
 1 file`,
			want: &PRMetrics{
				LinesChanged:  0,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"file1.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDiffStat(tt.diffOutput)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOrchestrator_executeRefactoring(t *testing.T) {
	tests := []struct {
		name            string
		initialWorktree string
		setupMocks      func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker)
		wantErr         bool
		wantNextPhase   Phase
	}{
		{
			name:            "fails when executor fails",
			initialWorktree: "/tmp/worktrees/test-workflow",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return((*ExecuteResult)(nil), errors.New("execution failed"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name:            "fails when LoadPlan fails",
			initialWorktree: "/tmp/worktrees/test-workflow",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return((*Plan)(nil), errors.New("plan not found"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name:            "fails when GenerateRefactoringPrompt fails",
			initialWorktree: "/tmp/worktrees/test-workflow",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("", errors.New("prompt generation failed"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name:            "fails when ExtractJSON fails",
			initialWorktree: "/tmp/worktrees/test-workflow",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "invalid output",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("", errors.New("no JSON found"))
				sm.On("SaveRawOutput", "test-workflow", PhaseRefactoring, "invalid output").Return(nil)
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflows/test-workflow")
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name:            "fails when ParseRefactoringSummary fails",
			initialWorktree: "/tmp/worktrees/test-workflow",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"invalid\": true}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"invalid\": true}", nil)
				op.On("ParseRefactoringSummary", mock.Anything).Return((*RefactoringSummary)(nil), errors.New("invalid summary"))
				sm.On("SaveRawOutput", "test-workflow", PhaseRefactoring, "```json\n{\"invalid\": true}\n```").Return(nil)
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflows/test-workflow")
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name:            "fails when SavePhaseOutput fails",
			initialWorktree: "/tmp/worktrees/test-workflow",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"refactored\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"refactored\"}", nil)
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{Summary: "refactored"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(errors.New("failed to save"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
		{
			name:            "fails when CI check fails",
			initialWorktree: "/tmp/worktrees/test-workflow",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"summary\": \"refactored\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"summary\": \"refactored\"}", nil)
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{Summary: "refactored"}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), errors.New("CI check error"))
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhaseRefactoring,
				WorktreePath: tt.initialWorktree,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusCompleted},
					PhaseRefactoring:    {Status: StatusInProgress},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.executeRefactoring(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantNextPhase, state.CurrentPhase)
			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executePRSplit(t *testing.T) {
	tests := []struct {
		name          string
		setupMocks    func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockGitRunner)
		wantErr       bool
		wantNextPhase Phase
	}{
		{
			name: "successfully splits PR",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, git *MockGitRunner) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				git.On("GetCurrentBranch", mock.Anything, mock.Anything).Return("feature-branch", nil)
				git.On("GetCommits", mock.Anything, mock.Anything, "main").Return([]command.Commit{
					{Hash: "abc123", Subject: "feat: add feature"},
				}, nil)
				pg.On("GeneratePRSplitPrompt", mock.Anything, mock.Anything).Return("pr-split prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"strategy\":\"commits\",\"parentTitle\":\"Parent\",\"parentDescription\":\"Desc\",\"summary\":\"split complete\",\"childPRs\":[]}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"strategy\":\"commits\",\"parentTitle\":\"Parent\",\"parentDescription\":\"Desc\",\"summary\":\"split complete\",\"childPRs\":[]}", nil)
				op.On("ParsePRSplitPlan", mock.Anything).Return(&PRSplitPlan{Strategy: SplitByCommits, ParentTitle: "Parent", ParentDesc: "Desc", Summary: "split complete", ChildPRs: []ChildPRPlan{}}, nil)
				sm.On("SavePhaseOutput", "test-workflow", PhasePRSplit, mock.Anything).Return(nil)
			},
			wantErr:       false,
			wantNextPhase: PhaseCompleted,
		},
		{
			name: "fails when metrics not available",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, git *MockGitRunner) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
			},
			wantErr:       true,
			wantNextPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockSplitMgr := new(MockPRSplitManager)
			mockGit := new(MockGitRunner)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockGit)

			if tt.name == "successfully splits PR" {
				mockSplitMgr.On("ExecuteSplit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&PRSplitResult{
					ParentPR: PRInfo{Number: 1, Title: "Parent"},
					ChildPRs: []PRInfo{},
					Summary:  "split complete",
				}, nil)
			}

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				gitRunner:       mockGit,
				splitManager:    mockSplitMgr,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
			}

			metrics := &PRMetrics{
				LinesChanged: 150,
				FilesChanged: 15,
			}

			var metricsPtr *PRMetrics
			if tt.name == "successfully splits PR" {
				metricsPtr = metrics
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhasePRSplit,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusCompleted},
					PhaseRefactoring:    {Status: StatusCompleted},
					PhasePRSplit:        {Status: StatusInProgress, Metrics: metricsPtr},
				},
			}

			err := o.executePRSplit(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantNextPhase, state.CurrentPhase)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executePRSplit_CIFailureRetry(t *testing.T) {
	// This test verifies that when CI fails on a child PR, the retry uses
	// GenerateFixCIPrompt to generate a proper fix prompt (not raw error text)
	mockSM := new(MockStateManager)
	mockExec := new(MockClaudeExecutor)
	mockPG := new(MockPromptGenerator)
	mockOP := new(MockOutputParser)
	mockCI := new(MockCIChecker)
	mockSplitMgr := new(MockPRSplitManager)
	mockGit := new(MockGitRunner)

	// Setup: first attempt splits PR, CI fails on child PR
	// second attempt uses GenerateFixCIPrompt, CI passes
	mockSM.On("SaveState", "test-workflow", mock.Anything).Return(nil)
	mockGit.On("GetCurrentBranch", mock.Anything, mock.Anything).Return("feature-branch", nil)
	mockGit.On("GetCommits", mock.Anything, mock.Anything, "main").Return([]command.Commit{
		{Hash: "abc123", Subject: "feat: add feature"},
	}, nil)

	// First attempt: GeneratePRSplitPrompt
	mockPG.On("GeneratePRSplitPrompt", mock.Anything, mock.Anything).Return("pr-split prompt", nil).Once()

	// First execution returns plan
	mockExec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
		return config.Prompt == "pr-split prompt"
	}), mock.Anything).Return(&ExecuteResult{
		Output:   `{"strategy":"commits","parentTitle":"Parent","parentDescription":"Desc","summary":"Split","childPRs":[{"title":"Child PR","description":"Desc"}]}`,
		ExitCode: 0,
	}, nil).Once()

	mockOP.On("ExtractJSON", mock.Anything).Return(`{"strategy":"commits","parentTitle":"Parent","parentDescription":"Desc","summary":"Split","childPRs":[{"title":"Child PR","description":"Desc"}]}`, nil)
	mockOP.On("ParsePRSplitPlan", mock.Anything).Return(&PRSplitPlan{
		Strategy:    SplitByCommits,
		ParentTitle: "Parent",
		ParentDesc:  "Desc",
		Summary:     "Split",
		ChildPRs:    []ChildPRPlan{{Title: "Child PR", Description: "Desc"}},
	}, nil)

	mockSplitMgr.On("ExecuteSplit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&PRSplitResult{
		ParentPR: PRInfo{Number: 1},
		ChildPRs: []PRInfo{{Number: 2, Title: "Child PR"}},
		Summary:  "Split",
	}, nil)
	mockSM.On("SavePhaseOutput", "test-workflow", PhasePRSplit, mock.Anything).Return(nil)

	// First CI check fails
	mockCI.On("WaitForCIWithProgress", mock.Anything, 2, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
		Passed:     false,
		Status:     "failure",
		FailedJobs: []string{"build"},
		Output:     "Build failed",
	}, nil).Once()

	// Second attempt: GenerateFixCIPrompt should be called (this is the fix we're testing)
	mockPG.On("GenerateFixCIPrompt", mock.Anything).Return("fix ci prompt", nil).Once()

	// Second execution with fix prompt
	mockExec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
		return config.Prompt == "fix ci prompt"
	}), mock.Anything).Return(&ExecuteResult{
		Output:   `{"strategy":"commits","parentTitle":"Parent","parentDescription":"Desc","summary":"Split","childPRs":[{"title":"Child PR","description":"Desc"}]}`,
		ExitCode: 0,
	}, nil).Once()

	// Second CI check passes
	mockCI.On("WaitForCIWithProgress", mock.Anything, 2, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
		Passed: true,
		Status: "success",
	}, nil).Once()

	config := DefaultConfig("/tmp/workflows")
	config.MaxFixAttempts = 3

	o := &Orchestrator{
		stateManager:    mockSM,
		executor:        mockExec,
		promptGenerator: mockPG,
		parser:          mockOP,
		gitRunner:       mockGit,
		splitManager:    mockSplitMgr,
		config:          config,
		logger:          NewLogger(LogLevelNormal),
		ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
			return mockCI
		},
	}

	state := &WorkflowState{
		Name:         "test-workflow",
		CurrentPhase: PhasePRSplit,
		Phases: map[Phase]*PhaseState{
			PhasePlanning:       {Status: StatusCompleted},
			PhaseConfirmation:   {Status: StatusCompleted},
			PhaseImplementation: {Status: StatusCompleted},
			PhaseRefactoring:    {Status: StatusCompleted},
			PhasePRSplit:        {Status: StatusInProgress, Metrics: &PRMetrics{LinesChanged: 150, FilesChanged: 15}},
		},
	}

	err := o.executePRSplit(context.Background(), state)

	require.NoError(t, err)
	assert.Equal(t, PhaseCompleted, state.CurrentPhase)

	// Verify GenerateFixCIPrompt was called (not just raw error passed)
	mockPG.AssertCalled(t, "GenerateFixCIPrompt", mock.Anything)
	mockSM.AssertExpectations(t)
	mockExec.AssertExpectations(t)
	mockPG.AssertExpectations(t)
}

func TestOrchestrator_Status(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager)
		want       *WorkflowState
		wantErr    bool
	}{
		{
			name: "successfully returns workflow status",
			setupMocks: func(sm *MockStateManager) {
				state := &WorkflowState{
					Name:         "test-workflow",
					CurrentPhase: PhasePlanning,
				}
				sm.On("LoadState", "test-workflow").Return(state, nil)
			},
			want: &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhasePlanning,
			},
			wantErr: false,
		},
		{
			name: "fails when workflow not found",
			setupMocks: func(sm *MockStateManager) {
				sm.On("LoadState", "test-workflow").Return((*WorkflowState)(nil), ErrWorkflowNotFound)
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				logger:       NewLogger(LogLevelNormal),
			}

			got, err := o.Status("test-workflow")

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_Delete(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager)
		wantErr    bool
	}{
		{
			name: "successfully deletes workflow",
			setupMocks: func(sm *MockStateManager) {
				sm.On("DeleteWorkflow", "test-workflow").Return(nil)
			},
			wantErr: false,
		},
		{
			name: "fails when workflow not found",
			setupMocks: func(sm *MockStateManager) {
				sm.On("DeleteWorkflow", "test-workflow").Return(ErrWorkflowNotFound)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				logger:       NewLogger(LogLevelNormal),
			}

			err := o.Delete("test-workflow")

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_failWorkflow(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		setupMocks func(*MockStateManager)
	}{
		{
			name: "successfully transitions to failed state",
			err:  errors.New("test error"),
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				logger:       NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhasePlanning,
				Phases: map[Phase]*PhaseState{
					PhasePlanning: {Status: StatusInProgress},
				},
			}

			err := o.failWorkflow(state, tt.err)

			require.Error(t, err)
			assert.Equal(t, PhaseFailed, state.CurrentPhase)
			assert.NotNil(t, state.Error)
			assert.Equal(t, tt.err.Error(), state.Error.Message)
			assert.Equal(t, FailureTypeExecution, state.Error.FailureType)
			assert.Equal(t, StatusFailed, state.Phases[PhasePlanning].Status)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_failWorkflowCI(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		setupMocks func(*MockStateManager)
	}{
		{
			name: "successfully transitions to failed state with CI failure type",
			err:  errors.New("ci check failed"),
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				logger:       NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhasePlanning,
				Phases: map[Phase]*PhaseState{
					PhasePlanning: {Status: StatusInProgress},
				},
			}

			err := o.failWorkflowCI(state, tt.err)

			require.Error(t, err)
			assert.Equal(t, PhaseFailed, state.CurrentPhase)
			assert.NotNil(t, state.Error)
			assert.Equal(t, tt.err.Error(), state.Error.Message)
			assert.Equal(t, FailureTypeCI, state.Error.FailureType)
			assert.Equal(t, StatusFailed, state.Phases[PhasePlanning].Status)
			mockSM.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_failWorkflowWithType(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		failureType FailureType
		setupMocks  func(*MockStateManager)
	}{
		{
			name:        "successfully transitions to failed state with execution failure type",
			err:         errors.New("execution failed"),
			failureType: FailureTypeExecution,
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
			},
		},
		{
			name:        "successfully transitions to failed state with CI failure type",
			err:         errors.New("ci check failed"),
			failureType: FailureTypeCI,
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
			},
		},
		{
			name:        "handles save state error",
			err:         errors.New("original error"),
			failureType: FailureTypeExecution,
			setupMocks: func(sm *MockStateManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(errors.New("save failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM)

			o := &Orchestrator{
				stateManager: mockSM,
				logger:       NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: PhasePlanning,
				Phases: map[Phase]*PhaseState{
					PhasePlanning: {Status: StatusInProgress},
				},
			}

			err := o.failWorkflowWithType(state, tt.err, tt.failureType)

			require.Error(t, err)
			assert.Equal(t, PhaseFailed, state.CurrentPhase)
			assert.NotNil(t, state.Error)
			assert.Equal(t, tt.failureType, state.Error.FailureType)
			assert.Equal(t, StatusFailed, state.Phases[PhasePlanning].Status)

			if tt.name == "handles save state error" {
				assert.Contains(t, err.Error(), "failed to save failed state")
				assert.Contains(t, err.Error(), "original error")
			} else {
				assert.Equal(t, tt.err.Error(), err.Error())
			}

			mockSM.AssertExpectations(t)
		})
	}
}

func TestDefaultConfirmFunc(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantApproved bool
		wantFeedback string
		wantErr      bool
		wantErrMsg   string
	}{
		{
			name:         "approves with y",
			input:        "y\n",
			wantApproved: true,
			wantFeedback: "",
			wantErr:      false,
		},
		{
			name:         "approves with yes",
			input:        "yes\n",
			wantApproved: true,
			wantFeedback: "",
			wantErr:      false,
		},
		{
			name:         "approves with Y uppercase",
			input:        "Y\n",
			wantApproved: true,
			wantFeedback: "",
			wantErr:      false,
		},
		{
			name:         "approves with YES uppercase",
			input:        "YES\n",
			wantApproved: true,
			wantFeedback: "",
			wantErr:      false,
		},
		{
			name:         "rejects with n",
			input:        "n\nmy feedback here\n",
			wantApproved: false,
			wantFeedback: "my feedback here",
			wantErr:      false,
		},
		{
			name:         "rejects with no",
			input:        "no\nanother feedback\n",
			wantApproved: false,
			wantFeedback: "another feedback",
			wantErr:      false,
		},
		{
			name:         "handles feedback input directly",
			input:        "please add more tests\n",
			wantApproved: false,
			wantFeedback: "please add more tests",
			wantErr:      false,
		},
		{
			name:         "handles empty input then valid input",
			input:        "\ny\n",
			wantApproved: true,
			wantFeedback: "",
			wantErr:      false,
		},
		{
			name:         "handles whitespace-only input then valid input",
			input:        "   \ny\n",
			wantApproved: true,
			wantFeedback: "",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a pipe to simulate stdin
			r, w, err := os.Pipe()
			require.NoError(t, err)
			defer r.Close()

			// Save original stdin and restore after test
			oldStdin := os.Stdin
			os.Stdin = r
			defer func() { os.Stdin = oldStdin }()

			// Write test input in a goroutine
			go func() {
				defer w.Close()
				w.WriteString(tt.input)
			}()

			plan := &Plan{
				Summary: "Test plan summary",
				Phases: []PlanPhase{
					{
						Name:           "Phase 1",
						Description:    "Test phase",
						EstimatedFiles: 1,
						EstimatedLines: 10,
					},
				},
			}

			approved, feedback, err := defaultConfirmFunc(plan)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantApproved, approved)
			assert.Equal(t, tt.wantFeedback, feedback)
		})
	}
}

func TestGetCIChecker(t *testing.T) {
	tests := []struct {
		name             string
		ciCheckerFactory func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker
		workingDir       string
		wantMock         bool
	}{
		{
			name: "uses factory when set",
			ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
				mockCI := new(MockCIChecker)
				return mockCI
			},
			workingDir: "/tmp/worktree",
			wantMock:   true,
		},
		{
			name:             "creates real checker when factory is nil",
			ciCheckerFactory: nil,
			workingDir:       "/tmp/worktree",
			wantMock:         false,
		},
		{
			name: "passes correct working directory to factory",
			ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
				assert.Equal(t, "/custom/worktree/path", workingDir)
				mockCI := new(MockCIChecker)
				return mockCI
			},
			workingDir: "/custom/worktree/path",
			wantMock:   true,
		},
		{
			name: "passes config check interval to factory",
			ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
				assert.Equal(t, 45*time.Second, checkInterval)
				mockCI := new(MockCIChecker)
				return mockCI
			},
			workingDir: "/tmp/worktree",
			wantMock:   true,
		},
		{
			name: "passes config command timeout to factory",
			ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
				assert.Equal(t, 3*time.Minute, commandTimeout)
				mockCI := new(MockCIChecker)
				return mockCI
			},
			workingDir: "/tmp/worktree",
			wantMock:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig("/tmp/workflows")
			if tt.name == "passes config check interval to factory" {
				config.CICheckInterval = 45 * time.Second
			}
			if tt.name == "passes config command timeout to factory" {
				config.GHCommandTimeout = 3 * time.Minute
			}

			o := &Orchestrator{
				config:           config,
				logger:           NewLogger(LogLevelNormal),
				ciCheckerFactory: tt.ciCheckerFactory,
			}

			checker := o.getCIChecker(tt.workingDir)

			assert.NotNil(t, checker)
			if tt.wantMock {
				_, ok := checker.(*MockCIChecker)
				assert.True(t, ok, "expected MockCIChecker")
			}
		})
	}
}

func TestOrchestrator_getPRMetrics(t *testing.T) {
	tests := []struct {
		name       string
		gitDiff    string
		want       *PRMetrics
		wantErr    bool
		setupMocks func(ctx context.Context, workingDir string)
	}{
		{
			name: "successfully parses git diff output",
			want: &PRMetrics{
				LinesChanged:  10,
				FilesChanged:  1,
				FilesAdded:    []string{},
				FilesModified: []string{"file.go"},
				FilesDeleted:  []string{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want != nil {
				metrics, err := parseDiffStat(" file.go | 10 ++++++++++\n 1 file changed, 10 insertions(+)")
				require.NoError(t, err)
				assert.Equal(t, tt.want, metrics)
			}
		})
	}
}

func TestFormatCIErrors(t *testing.T) {
	tests := []struct {
		name   string
		result *CIResult
		want   string
	}{
		{
			name: "formats single failed job",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "Build failed: syntax error",
				FailedJobs: []string{"build"},
			},
			want: "CI checks failed with the following errors:\n\nBuild failed: syntax error\n\nFailed jobs:\n- build\n",
		},
		{
			name: "formats multiple failed jobs",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "Multiple errors occurred",
				FailedJobs: []string{"build", "test", "lint"},
			},
			want: "CI checks failed with the following errors:\n\nMultiple errors occurred\n\nFailed jobs:\n- build\n- test\n- lint\n",
		},
		{
			name: "handles empty output",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "",
				FailedJobs: []string{"deploy"},
			},
			want: "CI checks failed with the following errors:\n\n\n\nFailed jobs:\n- deploy\n",
		},
		{
			name: "handles no failed jobs",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "Unknown error",
				FailedJobs: []string{},
			},
			want: "CI checks failed with the following errors:\n\nUnknown error",
		},
		{
			name: "formats output with multiline errors",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "Error 1: Build failed\nError 2: Test failed\nError 3: Lint failed",
				FailedJobs: []string{"ci"},
			},
			want: "CI checks failed with the following errors:\n\nError 1: Build failed\nError 2: Test failed\nError 3: Lint failed\n\nFailed jobs:\n- ci\n",
		},
		{
			name: "formats with special characters in output",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "Error: file \"test.go\" has issues\n\tLine 42: syntax error",
				FailedJobs: []string{"build"},
			},
			want: "CI checks failed with the following errors:\n\nError: file \"test.go\" has issues\n\tLine 42: syntax error\n\nFailed jobs:\n- build\n",
		},
		{
			name: "formats with nil failed jobs slice",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "CI system error",
				FailedJobs: nil,
			},
			want: "CI checks failed with the following errors:\n\nCI system error",
		},
		{
			name: "formats with long output",
			result: &CIResult{
				Passed: false,
				Status: "failure",
				Output: "Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
					"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
					"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris.",
				FailedJobs: []string{"integration-test"},
			},
			want: "CI checks failed with the following errors:\n\nLorem ipsum dolor sit amet, consectetur adipiscing elit. " +
				"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
				"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris.\n\nFailed jobs:\n- integration-test\n",
		},
		{
			name: "formats with job names containing spaces",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "Job failed",
				FailedJobs: []string{"build and test", "deploy to staging"},
			},
			want: "CI checks failed with the following errors:\n\nJob failed\n\nFailed jobs:\n- build and test\n- deploy to staging\n",
		},
		{
			name: "formats with job names containing special characters",
			result: &CIResult{
				Passed:     false,
				Status:     "failure",
				Output:     "Job failed",
				FailedJobs: []string{"build/test/deploy", "test:unit"},
			},
			want: "CI checks failed with the following errors:\n\nJob failed\n\nFailed jobs:\n- build/test/deploy\n- test:unit\n",
		},
		{
			name: "formats with cancelled jobs only",
			result: &CIResult{
				Passed:        false,
				Status:        "failure",
				Output:        "Infrastructure error",
				FailedJobs:    []string{},
				CancelledJobs: []string{"job1", "job2"},
			},
			want: "CI checks failed with the following errors:\n\nInfrastructure error\n\nCancelled jobs (infrastructure issue, not code failure):\n- job1\n- job2\n",
		},
		{
			name: "formats with both failed and cancelled jobs",
			result: &CIResult{
				Passed:        false,
				Status:        "failure",
				Output:        "Mixed errors",
				FailedJobs:    []string{"build"},
				CancelledJobs: []string{"deploy"},
			},
			want: "CI checks failed with the following errors:\n\nMixed errors\n\nFailed jobs:\n- build\n\nCancelled jobs (infrastructure issue, not code failure):\n- deploy\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCIErrors(tt.result)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOrchestrator_executePlanning_ParseErrors(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser)
		wantPhase  Phase
	}{
		{
			name: "fails when ExtractJSON fails and saves raw output",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				pg.On("GeneratePlanningPrompt", WorkflowTypeFeature, "test description", []string(nil)).Return("planning prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "some invalid output without json",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("", errors.New("no JSON found"))
				sm.On("SaveRawOutput", "test-workflow", PhasePlanning, "some invalid output without json").Return(nil)
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflows/test-workflow")
			},
			wantPhase: PhaseFailed,
		},
		{
			name: "fails when ParsePlan fails and saves raw output",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				pg.On("GeneratePlanningPrompt", WorkflowTypeFeature, "test description", []string(nil)).Return("planning prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "```json\n{\"invalid\": \"plan\"}\n```",
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return("{\"invalid\": \"plan\"}", nil)
				op.On("ParsePlan", mock.Anything).Return((*Plan)(nil), errors.New("invalid plan structure"))
				sm.On("SaveRawOutput", "test-workflow", PhasePlanning, "```json\n{\"invalid\": \"plan\"}\n```").Return(nil)
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflows/test-workflow")
			},
			wantPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test description",
				CurrentPhase: PhasePlanning,
				Phases: map[Phase]*PhaseState{
					PhasePlanning: {Status: StatusInProgress},
				},
			}

			err := o.executePlanning(context.Background(), state)

			require.Error(t, err)
			assert.Equal(t, tt.wantPhase, state.CurrentPhase)
			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
		})
	}
}

func TestHandleCancelledCI(t *testing.T) {
	tests := []struct {
		name            string
		ciResult        *CIResult
		setupMocks      func(*MockGhRunner, *MockCIChecker)
		wantPassed      bool
		wantErr         bool
		wantRerunCalled bool
	}{
		{
			name: "only cancelled jobs - rerun successful",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{},
				CancelledJobs: []string{"job1", "job2"},
				Output:        "test output",
			},
			setupMocks: func(gh *MockGhRunner, ci *MockCIChecker) {
				gh.On("GetLatestRunID", mock.Anything, "/test/dir", 123).Return(int64(456), nil)
				gh.On("RunRerun", mock.Anything, "/test/dir", int64(456)).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 123, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed:        true,
					Status:        "success",
					FailedJobs:    []string{},
					CancelledJobs: []string{},
					Output:        "rerun output",
				}, nil)
			},
			wantPassed:      true,
			wantErr:         false,
			wantRerunCalled: true,
		},
		{
			name: "only cancelled jobs - rerun still fails",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{},
				CancelledJobs: []string{"job1"},
				Output:        "test output",
			},
			setupMocks: func(gh *MockGhRunner, ci *MockCIChecker) {
				gh.On("GetLatestRunID", mock.Anything, "/test/dir", 123).Return(int64(456), nil)
				gh.On("RunRerun", mock.Anything, "/test/dir", int64(456)).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 123, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed:        false,
					Status:        "failure",
					FailedJobs:    []string{"job1"},
					CancelledJobs: []string{},
					Output:        "rerun failed",
				}, nil)
			},
			wantPassed:      false,
			wantErr:         false,
			wantRerunCalled: true,
		},
		{
			name: "has failed jobs - no rerun",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{"job1"},
				CancelledJobs: []string{"job2"},
				Output:        "test output",
			},
			setupMocks: func(gh *MockGhRunner, ci *MockCIChecker) {
				// No mocks should be called
			},
			wantPassed:      false,
			wantErr:         false,
			wantRerunCalled: false,
		},
		{
			name: "no cancelled jobs - no rerun",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{"job1"},
				CancelledJobs: []string{},
				Output:        "test output",
			},
			setupMocks: func(gh *MockGhRunner, ci *MockCIChecker) {
				// No mocks should be called
			},
			wantPassed:      false,
			wantErr:         false,
			wantRerunCalled: false,
		},
		{
			name: "GetLatestRunID fails - return error",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{},
				CancelledJobs: []string{"job1"},
				Output:        "test output",
			},
			setupMocks: func(gh *MockGhRunner, ci *MockCIChecker) {
				gh.On("GetLatestRunID", mock.Anything, "/test/dir", 123).Return(int64(0), errors.New("failed to get run ID"))
			},
			wantPassed:      false,
			wantErr:         true,
			wantRerunCalled: false,
		},
		{
			name: "RunRerun fails - return error",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{},
				CancelledJobs: []string{"job1"},
				Output:        "test output",
			},
			setupMocks: func(gh *MockGhRunner, ci *MockCIChecker) {
				gh.On("GetLatestRunID", mock.Anything, "/test/dir", 123).Return(int64(456), nil)
				gh.On("RunRerun", mock.Anything, "/test/dir", int64(456)).Return(errors.New("rerun failed"))
			},
			wantPassed:      false,
			wantErr:         true,
			wantRerunCalled: false,
		},
		{
			name: "WaitForCI fails - return error",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{},
				CancelledJobs: []string{"job1"},
				Output:        "test output",
			},
			setupMocks: func(gh *MockGhRunner, ci *MockCIChecker) {
				gh.On("GetLatestRunID", mock.Anything, "/test/dir", 123).Return(int64(456), nil)
				gh.On("RunRerun", mock.Anything, "/test/dir", int64(456)).Return(nil)
				ci.On("WaitForCIWithProgress", mock.Anything, 123, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), errors.New("CI check failed"))
			},
			wantPassed:      false,
			wantErr:         true,
			wantRerunCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGH := new(MockGhRunner)
			mockCI := new(MockCIChecker)

			tt.setupMocks(mockGH, mockCI)

			o := &Orchestrator{
				config:   DefaultConfig("/tmp/workflows"),
				logger:   NewLogger(LogLevelNormal),
				ghRunner: mockGH,
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			result, err := o.handleCancelledCI(context.Background(), 123, "/test/dir", tt.ciResult)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.wantRerunCalled {
				assert.Equal(t, tt.wantPassed, result.Passed)
			} else {
				// If no rerun, result should be the same as input
				assert.Equal(t, tt.ciResult, result)
			}

			mockGH.AssertExpectations(t)
			mockCI.AssertExpectations(t)
		})
	}
}

func TestDisplayCIFailure(t *testing.T) {
	tests := []struct {
		name     string
		ciResult *CIResult
	}{
		{
			name: "only failed jobs",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{"test-unit", "test-integration"},
				CancelledJobs: []string{},
				Output:        "Test failures detected",
			},
		},
		{
			name: "only cancelled jobs",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{},
				CancelledJobs: []string{"build", "deploy"},
				Output:        "Jobs were cancelled",
			},
		},
		{
			name: "both failed and cancelled jobs",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{"test-unit"},
				CancelledJobs: []string{"deploy"},
				Output:        "Mixed failures and cancellations",
			},
		},
		{
			name: "no failed or cancelled jobs",
			ciResult: &CIResult{
				Passed:        false,
				Status:        "failure",
				FailedJobs:    []string{},
				CancelledJobs: []string{},
				Output:        "Unknown failure",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			displayCIFailure(tt.ciResult)
		})
	}
}

func TestPRCreationResult_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    PRCreationResult
		wantErr bool
	}{
		{
			name:  "created status with PR number",
			input: `{"prNumber": 123, "status": "created", "message": "PR created successfully"}`,
			want: PRCreationResult{
				PRNumber: 123,
				Status:   "created",
				Message:  "PR created successfully",
			},
			wantErr: false,
		},
		{
			name:  "exists status with PR number",
			input: `{"prNumber": 456, "status": "exists", "message": "PR already exists"}`,
			want: PRCreationResult{
				PRNumber: 456,
				Status:   "exists",
				Message:  "PR already exists",
			},
			wantErr: false,
		},
		{
			name:  "skipped status without PR number",
			input: `{"prNumber": 0, "status": "skipped", "message": "No commits to create PR"}`,
			want: PRCreationResult{
				PRNumber: 0,
				Status:   "skipped",
				Message:  "No commits to create PR",
			},
			wantErr: false,
		},
		{
			name:  "failed status",
			input: `{"prNumber": 0, "status": "failed", "message": "Pre-commit hooks failed"}`,
			want: PRCreationResult{
				PRNumber: 0,
				Status:   "failed",
				Message:  "Pre-commit hooks failed",
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got PRCreationResult
			err := json.Unmarshal([]byte(tt.input), &got)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOrchestrator_ExecutePRCreation(t *testing.T) {
	tests := []struct {
		name         string
		setupMocks   func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser)
		state        *WorkflowState
		wantPRNumber int
		wantErr      bool
		errContains  string
	}{
		{
			name: "successful PR creation",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch == "feature/test" && ctx.WorkflowType == WorkflowTypeFeature && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "create pr prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 123, "status": "created", "message": "PR created successfully"}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 123, "status": "created", "message": "PR created successfully"}`, nil)
			},
			state: &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				WorktreePath: "/tmp/worktree",
			},
			wantPRNumber: 123,
			wantErr:      false,
		},
		{
			name: "PR already exists",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 456, "status": "exists", "message": "PR already exists"}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 456, "status": "exists", "message": "PR already exists"}`, nil)
			},
			state: &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				WorktreePath: "/tmp/worktree",
			},
			wantPRNumber: 456,
			wantErr:      false,
		},
		{
			name: "PR creation skipped (no commits)",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 0, "status": "skipped", "message": "No commits on branch"}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 0, "status": "skipped", "message": "No commits on branch"}`, nil)
			},
			state: &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				WorktreePath: "/tmp/worktree",
			},
			wantPRNumber: 0,
			wantErr:      false,
		},
		{
			name: "PR creation failed after max retries",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 0, "status": "failed", "message": "Pre-commit hooks failed"}`,
					ExitCode: 0,
				}, nil).Times(3)

				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 0, "status": "failed", "message": "Pre-commit hooks failed"}`, nil).Times(3)
			},
			state: &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				WorktreePath: "/tmp/worktree",
			},
			wantPRNumber: 0,
			wantErr:      true,
			errContains:  "failed to create PR after 3 attempts",
		},
		{
			name: "executor fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return((*ExecuteResult)(nil), errors.New("execution error")).Times(3)
			},
			state: &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				WorktreePath: "/tmp/worktree",
			},
			wantPRNumber: 0,
			wantErr:      true,
			errContains:  "failed to create PR after 3 attempts",
		},
		{
			name: "JSON extraction fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(&ExecuteResult{
					Output:   "no json here",
					ExitCode: 0,
				}, nil).Times(3)

				op.On("ExtractJSON", mock.Anything).Return("", errors.New("no JSON found")).Times(3)
			},
			state: &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				WorktreePath: "/tmp/worktree",
			},
			wantPRNumber: 0,
			wantErr:      true,
			errContains:  "failed to create PR after 3 attempts",
		},
		{
			name: "uses baseDir when worktreePath is empty",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser) {
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.WorkingDirectory != "" && config.Prompt == "create pr prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 789, "status": "created", "message": "PR created"}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 789, "status": "created", "message": "PR created"}`, nil)
			},
			state: &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				WorktreePath: "",
			},
			wantPRNumber: 789,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP)

			cmdRunner := command.NewRunner()
			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/base"),
				logger:          NewLogger(LogLevelNormal),
				gitRunner:       command.NewGitRunner(cmdRunner),
			}

			// Create a temporary directory for git command to work
			tmpDir := t.TempDir()
			cmd := exec.Command("git", "init")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "config", "user.name", "Test User")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "config", "user.email", "test@example.com")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			// Create initial commit to allow branch checkout
			cmd = exec.Command("git", "commit", "--allow-empty", "-m", "initial commit")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "checkout", "-b", "feature/test")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			// Update state to use temp dir
			if tt.state.WorktreePath != "" {
				tt.state.WorktreePath = tmpDir
			} else {
				o.config.BaseDir = tmpDir
			}

			prNumber, err := o.executePRCreation(context.Background(), tt.state)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPRNumber, prNumber)

			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_ExecuteImplementation_NoPRError(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker, *MockWorktreeManager)
		wantErr    bool
		wantPhase  Phase
	}{
		{
			name: "NoPRError triggers PR creation and CI passes",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wt *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{
					Summary: "test plan",
				}, nil)

				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "implementation prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"summary": "implemented", "filesChanged": [], "linesAdded": 100, "linesRemoved": 50, "testsAdded": 3}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"summary": "implemented", "filesChanged": [], "linesAdded": 100, "linesRemoved": 50, "testsAdded": 3}`, nil).Once()
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{
					Summary:      "implemented",
					LinesAdded:   100,
					LinesRemoved: 50,
					TestsAdded:   3,
				}, nil)

				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)

				// First CI check returns NoPRError
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), &NoPRError{
					Branch: "feature/test",
					Msg:    "no PR found",
				}).Once()

				// PR creation prompt and execution
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "create pr prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 123, "status": "created", "message": "PR created"}`,
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 123, "status": "created", "message": "PR created"}`, nil).Once()

				// Second CI check passes after PR creation
				ci.On("WaitForCIWithProgress", mock.Anything, 123, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed: true,
					Status: "success",
				}, nil)
			},
			wantErr:   false,
			wantPhase: PhaseRefactoring,
		},
		{
			name: "NoPRError with PR creation failure",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wt *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{
					Summary: "test plan",
				}, nil)

				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "implementation prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"summary": "implemented", "filesChanged": [], "linesAdded": 100, "linesRemoved": 50, "testsAdded": 3}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"summary": "implemented", "filesChanged": [], "linesAdded": 100, "linesRemoved": 50, "testsAdded": 3}`, nil).Once()
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{
					Summary:      "implemented",
					LinesAdded:   100,
					LinesRemoved: 50,
					TestsAdded:   3,
				}, nil)

				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)

				// CI check returns NoPRError
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), &NoPRError{
					Branch: "feature/test",
					Msg:    "no PR found",
				}).Once()

				// PR creation fails after retries
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "create pr prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 0, "status": "failed", "message": "hooks failed"}`,
					ExitCode: 0,
				}, nil).Times(3)
				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 0, "status": "failed", "message": "hooks failed"}`, nil).Times(3)
			},
			wantErr:   true,
			wantPhase: PhaseFailed,
		},
		{
			name: "NoPRError with PR skipped (no commits)",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wt *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{
					Summary: "test plan",
				}, nil)

				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "implementation prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"summary": "implemented", "filesChanged": [], "linesAdded": 100, "linesRemoved": 50, "testsAdded": 3}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"summary": "implemented", "filesChanged": [], "linesAdded": 100, "linesRemoved": 50, "testsAdded": 3}`, nil).Once()
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{
					Summary:      "implemented",
					LinesAdded:   100,
					LinesRemoved: 50,
					TestsAdded:   3,
				}, nil)

				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)

				// CI check returns NoPRError
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), &NoPRError{
					Branch: "feature/test",
					Msg:    "no PR found",
				}).Once()

				// PR creation returns skipped
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "create pr prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 0, "status": "skipped", "message": "no commits"}`,
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 0, "status": "skipped", "message": "no commits"}`, nil).Once()
			},
			wantErr:   true,
			wantPhase: PhaseFailed,
		},
		{
			name: "regular CI error (not NoPRError)",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker, wt *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{
					Summary: "test plan",
				}, nil)

				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "implementation prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"summary": "implemented", "filesChanged": [], "linesAdded": 100, "linesRemoved": 50, "testsAdded": 3}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"summary": "implemented", "filesChanged": [], "linesAdded": 100, "linesRemoved": 50, "testsAdded": 3}`, nil)
				op.On("ParseImplementationSummary", mock.Anything).Return(&ImplementationSummary{
					Summary:      "implemented",
					LinesAdded:   100,
					LinesRemoved: 50,
					TestsAdded:   3,
				}, nil)

				sm.On("SavePhaseOutput", "test-workflow", PhaseImplementation, mock.Anything).Return(nil)

				// Regular CI error (not NoPRError)
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), errors.New("gh not installed"))
			},
			wantErr:   true,
			wantPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)
			mockWT := new(MockWorktreeManager)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI, mockWT)

			// Create a temporary directory for git operations
			tmpDir := t.TempDir()
			cmd := exec.Command("git", "init")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "config", "user.name", "Test User")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "config", "user.email", "test@example.com")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			// Create initial commit to allow branch checkout
			cmd = exec.Command("git", "commit", "--allow-empty", "-m", "initial commit")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "checkout", "-b", "feature/test")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmdRunner := command.NewRunner()
			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				worktreeManager: mockWT,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
				gitRunner:       command.NewGitRunner(cmdRunner),
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				CurrentPhase: PhaseImplementation,
				WorktreePath: tmpDir,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusInProgress},
					PhaseRefactoring:    {Status: StatusPending},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.executeImplementation(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPhase, state.CurrentPhase)

			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
			mockCI.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_ExecuteRefactoring_NoPRError(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockOutputParser, *MockCIChecker)
		wantErr    bool
		wantPhase  Phase
	}{
		{
			name: "NoPRError triggers PR creation and CI passes",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{
					Summary: "test plan",
				}, nil)

				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "refactoring prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"summary": "refactored", "filesChanged": [], "improvementsMade": ["improvement 1"]}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"summary": "refactored", "filesChanged": [], "improvementsMade": ["improvement 1"]}`, nil).Once()
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{
					Summary:          "refactored",
					FilesChanged:     []string{},
					ImprovementsMade: []string{"improvement 1"},
				}, nil)

				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil)

				// First CI check returns NoPRError
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), &NoPRError{
					Branch: "feature/test",
					Msg:    "no PR found",
				}).Once()

				// PR creation prompt and execution
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "create pr prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 456, "status": "created", "message": "PR created"}`,
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 456, "status": "created", "message": "PR created"}`, nil).Once()

				// Second CI check passes after PR creation
				ci.On("WaitForCIWithProgress", mock.Anything, 456, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed: true,
					Status: "success",
				}, nil)
			},
			wantErr:   false,
			wantPhase: PhaseCompleted,
		},
		{
			name: "NoPRError with PR creation returning existing PR",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{
					Summary: "test plan",
				}, nil)

				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "refactoring prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"summary": "refactored", "filesChanged": [], "improvementsMade": ["improvement 1"]}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"summary": "refactored", "filesChanged": [], "improvementsMade": ["improvement 1"]}`, nil).Once()
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{
					Summary:          "refactored",
					FilesChanged:     []string{},
					ImprovementsMade: []string{"improvement 1"},
				}, nil)

				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil)

				// CI check returns NoPRError
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), &NoPRError{
					Branch: "feature/test",
					Msg:    "no PR found",
				}).Once()

				// PR already exists
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "create pr prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"prNumber": 789, "status": "exists", "message": "PR already exists"}`,
					ExitCode: 0,
				}, nil)
				op.On("ExtractJSON", mock.Anything).Return(`{"prNumber": 789, "status": "exists", "message": "PR already exists"}`, nil).Once()

				// CI check passes with existing PR
				ci.On("WaitForCIWithProgress", mock.Anything, 789, mock.Anything, mock.Anything, mock.Anything).Return(&CIResult{
					Passed: true,
					Status: "success",
				}, nil)
			},
			wantErr:   false,
			wantPhase: PhaseCompleted,
		},
		{
			name: "NoPRError but PR creation fails",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, op *MockOutputParser, ci *MockCIChecker) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{
					Summary: "test plan",
				}, nil)

				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)

				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "refactoring prompt"
				}), mock.Anything).Return(&ExecuteResult{
					Output:   `{"summary": "refactored", "filesChanged": [], "improvementsMade": ["improvement 1"]}`,
					ExitCode: 0,
				}, nil)

				op.On("ExtractJSON", mock.Anything).Return(`{"summary": "refactored", "filesChanged": [], "improvementsMade": ["improvement 1"]}`, nil)
				op.On("ParseRefactoringSummary", mock.Anything).Return(&RefactoringSummary{
					Summary:          "refactored",
					FilesChanged:     []string{},
					ImprovementsMade: []string{"improvement 1"},
				}, nil)

				sm.On("SavePhaseOutput", "test-workflow", PhaseRefactoring, mock.Anything).Return(nil)

				// CI check returns NoPRError
				ci.On("WaitForCIWithProgress", mock.Anything, 0, mock.Anything, mock.Anything, mock.Anything).Return((*CIResult)(nil), &NoPRError{
					Branch: "feature/test",
					Msg:    "no PR found",
				})

				// PR creation fails
				pg.On("GenerateCreatePRPrompt", mock.MatchedBy(func(ctx *PRCreationContext) bool {
					return ctx.Branch != "" && ctx.BaseBranch != ""
				})).Return("create pr prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.MatchedBy(func(config ExecuteConfig) bool {
					return config.Prompt == "create pr prompt"
				}), mock.Anything).Return((*ExecuteResult)(nil), errors.New("execution failed")).Times(3)
			},
			wantErr:   true,
			wantPhase: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockOP := new(MockOutputParser)
			mockCI := new(MockCIChecker)

			tt.setupMocks(mockSM, mockExec, mockPG, mockOP, mockCI)

			// Create a temporary directory for git operations
			tmpDir := t.TempDir()
			cmd := exec.Command("git", "init")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "config", "user.name", "Test User")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "config", "user.email", "test@example.com")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			// Create initial commit to allow branch checkout
			cmd = exec.Command("git", "commit", "--allow-empty", "-m", "initial commit")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmd = exec.Command("git", "checkout", "-b", "feature/test")
			cmd.Dir = tmpDir
			require.NoError(t, cmd.Run())

			cmdRunner := command.NewRunner()
			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				parser:          mockOP,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
				gitRunner:       command.NewGitRunner(cmdRunner),
				ciCheckerFactory: func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker {
					return mockCI
				},
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test feature",
				CurrentPhase: PhaseRefactoring,
				WorktreePath: tmpDir,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusCompleted},
					PhaseRefactoring:    {Status: StatusInProgress},
					PhasePRSplit:        {Status: StatusPending},
				},
			}

			err := o.executeRefactoring(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPhase, state.CurrentPhase)

			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockOP.AssertExpectations(t)
			mockCI.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executePlanning_PromptTooLong(t *testing.T) {
	tests := []struct {
		name            string
		setupMocks      func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator)
		wantErr         bool
		wantPhase       Phase
		wantFeedbackLen int
	}{
		{
			name: "returns nil on ErrPromptTooLong to trigger retry",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				pg.On("GeneratePlanningPrompt", WorkflowTypeFeature, "test description", []string(nil)).Return("planning prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					fmt.Errorf("claude execution failed with exit code 1: %w", ErrPromptTooLong),
				)
			},
			wantErr:         false,
			wantPhase:       PhasePlanning,
			wantFeedbackLen: 1,
		},
		{
			name: "returns error on other errors",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				pg.On("GeneratePlanningPrompt", WorkflowTypeFeature, "test description", []string(nil)).Return("planning prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					errors.New("some other error"),
				)
			},
			wantErr:         true,
			wantPhase:       PhaseFailed,
			wantFeedbackLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)

			tt.setupMocks(mockSM, mockExec, mockPG)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test description",
				CurrentPhase: PhasePlanning,
				Phases: map[Phase]*PhaseState{
					PhasePlanning: {Status: StatusInProgress},
				},
			}

			err := o.executePlanning(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPhase, state.CurrentPhase)
			assert.Len(t, state.Phases[PhasePlanning].Feedback, tt.wantFeedbackLen)
			if tt.wantFeedbackLen > 0 {
				assert.Contains(t, state.Phases[PhasePlanning].Feedback[0], "too long")
			}

			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executeImplementation_PromptTooLong(t *testing.T) {
	tests := []struct {
		name            string
		setupMocks      func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockWorktreeManager)
		wantErr         bool
		wantPhase       Phase
		wantFeedbackLen int
	}{
		{
			name: "returns nil on ErrPromptTooLong to trigger retry",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					fmt.Errorf("claude execution failed with exit code 1: %w", ErrPromptTooLong),
				)
			},
			wantErr:         false,
			wantPhase:       PhaseImplementation,
			wantFeedbackLen: 1,
		},
		{
			name: "returns error on other errors",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, wm *MockWorktreeManager) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateImplementationPrompt", mock.Anything).Return("implementation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					errors.New("some other error"),
				)
			},
			wantErr:         true,
			wantPhase:       PhaseFailed,
			wantFeedbackLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockWM := new(MockWorktreeManager)

			tt.setupMocks(mockSM, mockExec, mockPG, mockWM)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				worktreeManager: mockWM,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test description",
				CurrentPhase: PhaseImplementation,
				WorktreePath: "/tmp/worktree",
				Phases: map[Phase]*PhaseState{
					PhaseImplementation: {Status: StatusInProgress},
				},
			}

			err := o.executeImplementation(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPhase, state.CurrentPhase)
			assert.Len(t, state.Phases[PhaseImplementation].Feedback, tt.wantFeedbackLen)
			if tt.wantFeedbackLen > 0 {
				assert.Contains(t, state.Phases[PhaseImplementation].Feedback[0], "too long")
			}

			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executeRefactoring_PromptTooLong(t *testing.T) {
	tests := []struct {
		name            string
		setupMocks      func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator)
		wantErr         bool
		wantPhase       Phase
		wantFeedbackLen int
	}{
		{
			name: "returns nil on ErrPromptTooLong to trigger retry",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					fmt.Errorf("claude execution failed with exit code 1: %w", ErrPromptTooLong),
				)
			},
			wantErr:         false,
			wantPhase:       PhaseRefactoring,
			wantFeedbackLen: 1,
		},
		{
			name: "returns error on other errors",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				sm.On("LoadPlan", "test-workflow").Return(&Plan{Summary: "test plan"}, nil)
				pg.On("GenerateRefactoringPrompt", mock.Anything).Return("refactoring prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					errors.New("some other error"),
				)
			},
			wantErr:         true,
			wantPhase:       PhaseFailed,
			wantFeedbackLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)

			tt.setupMocks(mockSM, mockExec, mockPG)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test description",
				CurrentPhase: PhaseRefactoring,
				WorktreePath: "/tmp/worktree",
				Phases: map[Phase]*PhaseState{
					PhaseRefactoring: {Status: StatusInProgress},
				},
			}

			err := o.executeRefactoring(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPhase, state.CurrentPhase)
			assert.Len(t, state.Phases[PhaseRefactoring].Feedback, tt.wantFeedbackLen)
			if tt.wantFeedbackLen > 0 {
				assert.Contains(t, state.Phases[PhaseRefactoring].Feedback[0], "too long")
			}

			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executePRSplit_PromptTooLong(t *testing.T) {
	tests := []struct {
		name            string
		setupMocks      func(*MockStateManager, *MockClaudeExecutor, *MockPromptGenerator, *MockGitRunner)
		wantErr         bool
		wantPhase       Phase
		wantFeedbackLen int
	}{
		{
			name: "returns nil on ErrPromptTooLong to trigger retry",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, gr *MockGitRunner) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				gr.On("GetCurrentBranch", mock.Anything, "/tmp/worktree").Return("test-branch", nil)
				gr.On("GetCommits", mock.Anything, "/tmp/worktree", "main").Return([]command.Commit{}, nil)
				pg.On("GeneratePRSplitPrompt", mock.Anything, mock.Anything).Return("pr split prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					fmt.Errorf("claude execution failed with exit code 1: %w", ErrPromptTooLong),
				)
			},
			wantErr:         false,
			wantPhase:       PhasePRSplit,
			wantFeedbackLen: 1,
		},
		{
			name: "returns error on other errors",
			setupMocks: func(sm *MockStateManager, exec *MockClaudeExecutor, pg *MockPromptGenerator, gr *MockGitRunner) {
				sm.On("SaveState", "test-workflow", mock.Anything).Return(nil).Times(2)
				gr.On("GetCurrentBranch", mock.Anything, "/tmp/worktree").Return("test-branch", nil)
				gr.On("GetCommits", mock.Anything, "/tmp/worktree", "main").Return([]command.Commit{}, nil)
				pg.On("GeneratePRSplitPrompt", mock.Anything, mock.Anything).Return("pr split prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					errors.New("some other error"),
				)
			},
			wantErr:         true,
			wantPhase:       PhaseFailed,
			wantFeedbackLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSM := new(MockStateManager)
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockGR := new(MockGitRunner)

			tt.setupMocks(mockSM, mockExec, mockPG, mockGR)

			o := &Orchestrator{
				stateManager:    mockSM,
				executor:        mockExec,
				promptGenerator: mockPG,
				gitRunner:       mockGR,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test description",
				CurrentPhase: PhasePRSplit,
				WorktreePath: "/tmp/worktree",
				Phases: map[Phase]*PhaseState{
					PhasePRSplit: {
						Status:  StatusInProgress,
						Metrics: &PRMetrics{LinesChanged: 100, FilesChanged: 10},
					},
				},
			}

			err := o.executePRSplit(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPhase, state.CurrentPhase)
			assert.Len(t, state.Phases[PhasePRSplit].Feedback, tt.wantFeedbackLen)
			if tt.wantFeedbackLen > 0 {
				assert.Contains(t, state.Phases[PhasePRSplit].Feedback[0], "too long")
			}

			mockSM.AssertExpectations(t)
			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockGR.AssertExpectations(t)
		})
	}
}

func TestOrchestrator_executePRCreation_PromptTooLong(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockClaudeExecutor, *MockPromptGenerator, *MockGitRunner)
		wantErr    bool
		wantPRNum  int
	}{
		{
			name: "returns error immediately on ErrPromptTooLong without retry",
			setupMocks: func(exec *MockClaudeExecutor, pg *MockPromptGenerator, gr *MockGitRunner) {
				gr.On("GetCurrentBranch", mock.Anything, "/tmp/worktree").Return("test-branch", nil)
				pg.On("GenerateCreatePRPrompt", mock.Anything).Return("pr creation prompt", nil)
				exec.On("ExecuteStreaming", mock.Anything, mock.Anything, mock.Anything).Return(
					(*ExecuteResult)(nil),
					fmt.Errorf("claude execution failed with exit code 1: %w", ErrPromptTooLong),
				)
			},
			wantErr:   true,
			wantPRNum: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExec := new(MockClaudeExecutor)
			mockPG := new(MockPromptGenerator)
			mockGR := new(MockGitRunner)

			tt.setupMocks(mockExec, mockPG, mockGR)

			o := &Orchestrator{
				executor:        mockExec,
				promptGenerator: mockPG,
				gitRunner:       mockGR,
				config:          DefaultConfig("/tmp/workflows"),
				logger:          NewLogger(LogLevelNormal),
			}

			state := &WorkflowState{
				Name:         "test-workflow",
				Type:         WorkflowTypeFeature,
				Description:  "test description",
				WorktreePath: "/tmp/worktree",
			}

			prNum, err := o.executePRCreation(context.Background(), state)

			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, errors.Is(err, ErrPromptTooLong))
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPRNum, prNum)

			mockExec.AssertExpectations(t)
			mockPG.AssertExpectations(t)
			mockGR.AssertExpectations(t)
		})
	}
}


func TestOrchestrator_handleSkipToPhase(t *testing.T) {
	tests := []struct {
		name             string
		currentPhase     Phase
		targetPhase      Phase
		forceBackward    bool
		externalPlanPath string
		setupState       func(*WorkflowState)
		setupMocks       func(*MockStateManager, string)
		wantErr          bool
		errContains      string
		validateState    func(*testing.T, *WorkflowState)
	}{
		{
			name:             "skip forward with external plan - planning to confirmation",
			currentPhase:     PhasePlanning,
			targetPhase:      PhaseConfirmation,
			externalPlanPath: "/tmp/plan.json",
			setupState: func(state *WorkflowState) {
				state.Phases[PhasePlanning].Status = StatusInProgress
				state.Phases[PhaseConfirmation].Status = StatusPending
			},
			setupMocks: func(sm *MockStateManager, planPath string) {
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflow")
				sm.On("SavePlan", "test-workflow", mock.AnythingOfType("*workflow.Plan")).Return(nil)
				sm.On("SavePlanMarkdown", "test-workflow", mock.AnythingOfType("string")).Return(nil)
			},
			wantErr: false,
			validateState: func(t *testing.T, state *WorkflowState) {
				assert.Equal(t, PhaseConfirmation, state.CurrentPhase)
				assert.True(t, state.ExternalPlanUsed)
				// No phases skipped when going from 0 to 1
				assert.Len(t, state.SkippedPhases, 0)
				assert.Equal(t, StatusInProgress, state.Phases[PhaseConfirmation].Status)
				require.Len(t, state.PhaseHistory, 1)
				assert.Equal(t, "skip", state.PhaseHistory[0].TransitionType)
			},
		},
		{
			name:         "skip forward planning to implementation - skips confirmation",
			currentPhase: PhasePlanning,
			targetPhase:  PhaseImplementation,
			externalPlanPath: "/tmp/plan.json",
			setupState: func(state *WorkflowState) {
				state.Phases[PhasePlanning].Status = StatusInProgress
				state.Phases[PhaseConfirmation].Status = StatusCompleted
				state.Phases[PhaseImplementation].Status = StatusPending
			},
			setupMocks: func(sm *MockStateManager, planPath string) {
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflow")
				sm.On("SavePlan", "test-workflow", mock.AnythingOfType("*workflow.Plan")).Return(nil)
				sm.On("SavePlanMarkdown", "test-workflow", mock.AnythingOfType("string")).Return(nil)
			},
			wantErr: false,
			validateState: func(t *testing.T, state *WorkflowState) {
				assert.Equal(t, PhaseImplementation, state.CurrentPhase)
				assert.True(t, state.ExternalPlanUsed)
				// Only confirmation is skipped (between 0 and 2)
				assert.Contains(t, state.SkippedPhases, PhaseConfirmation)
				assert.Len(t, state.SkippedPhases, 1)
				assert.Equal(t, StatusSkipped, state.Phases[PhaseConfirmation].Status)
				assert.Equal(t, StatusInProgress, state.Phases[PhaseImplementation].Status)
			},
		},
		{
			name:         "validation fails when prerequisites missing",
			currentPhase: PhasePlanning,
			targetPhase:  PhaseImplementation,
			setupState: func(state *WorkflowState) {
				state.Phases[PhasePlanning].Status = StatusInProgress
				state.Phases[PhaseConfirmation].Status = StatusPending
				state.Phases[PhaseImplementation].Status = StatusPending
			},
			setupMocks: func(sm *MockStateManager, planPath string) {
				sm.On("WorkflowDir", "test-workflow").Return("/tmp/workflow")
			},
			wantErr:     true,
			errContains: "missing prerequisites",
		},
		{
			name:         "validation fails when skipping to COMPLETED",
			currentPhase: PhasePlanning,
			targetPhase:  PhaseCompleted,
			setupState: func(state *WorkflowState) {
			},
			setupMocks: func(sm *MockStateManager, planPath string) {
			},
			wantErr:     true,
			errContains: "cannot skip to COMPLETED",
		},
		{
			name:          "validation fails for backward skip without force",
			currentPhase:  PhaseImplementation,
			targetPhase:   PhasePlanning,
			forceBackward: false,
			setupState: func(state *WorkflowState) {
			},
			setupMocks: func(sm *MockStateManager, planPath string) {
				// No mocks needed - validation fails early
			},
			wantErr:     true,
			errContains: "cannot skip backward",
		},
		{
			name:          "validation succeeds for backward skip with force",
			currentPhase:  PhaseImplementation,
			targetPhase:   PhasePlanning,
			forceBackward: true,
			setupState: func(state *WorkflowState) {
			},
			setupMocks: func(sm *MockStateManager, planPath string) {
				// Planning has no prerequisites, so no mocks needed
			},
			wantErr: false,
			validateState: func(t *testing.T, state *WorkflowState) {
				assert.Equal(t, PhasePlanning, state.CurrentPhase)
				assert.Equal(t, StatusInProgress, state.Phases[PhasePlanning].Status)
				// Backward skip doesn't mark phases as skipped
				assert.Len(t, state.SkippedPhases, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var planPath string
			if tt.externalPlanPath != "" {
				tmpDir := t.TempDir()
				plan := &Plan{
					Summary: "Test plan",
					Phases: []PlanPhase{
						{Name: "Phase 1", Description: "Test phase"},
					},
					WorkStreams: []WorkStream{
						{Name: "Stream 1", Tasks: []string{"Task 1"}},
					},
				}
				planBytes, err := json.Marshal(plan)
				require.NoError(t, err)
				planPath = tmpDir + "/plan.json"
				err = os.WriteFile(planPath, planBytes, 0644)
				require.NoError(t, err)
			}

			mockSM := new(MockStateManager)
			tt.setupMocks(mockSM, planPath)

			state := &WorkflowState{
				Name:         "test-workflow",
				CurrentPhase: tt.currentPhase,
				Phases: map[Phase]*PhaseState{
					PhasePlanning:       {Status: StatusCompleted},
					PhaseConfirmation:   {Status: StatusCompleted},
					PhaseImplementation: {Status: StatusCompleted},
					PhaseRefactoring:    {Status: StatusPending},
				},
				SkippedPhases: []Phase{},
				PhaseHistory:  []PhaseTransition{},
			}

			if tt.setupState != nil {
				tt.setupState(state)
			}

			o := &Orchestrator{
				stateManager: mockSM,
				config:       DefaultConfig("/tmp/workflows"),
				logger:       NewLogger(LogLevelNormal),
			}

			err := o.handleSkipToPhase(state, tt.targetPhase, tt.forceBackward, planPath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				if tt.validateState != nil {
					tt.validateState(t, state)
				}
			}

			mockSM.AssertExpectations(t)
		})
	}
}
