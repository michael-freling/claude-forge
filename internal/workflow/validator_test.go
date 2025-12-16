package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateWorkflowName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid simple name",
			input:   "auth-feature",
			wantErr: false,
		},
		{
			name:    "valid name with numbers",
			input:   "feature-123",
			wantErr: false,
		},
		{
			name:    "valid name all lowercase",
			input:   "myfeature",
			wantErr: false,
		},
		{
			name:    "valid name all uppercase",
			input:   "MYFEATURE",
			wantErr: false,
		},
		{
			name:    "valid name mixed case",
			input:   "MyFeature",
			wantErr: false,
		},
		{
			name:    "valid name with multiple hyphens",
			input:   "my-auth-feature",
			wantErr: false,
		},
		{
			name:    "valid single character",
			input:   "a",
			wantErr: false,
		},
		{
			name:    "valid two characters",
			input:   "ab",
			wantErr: false,
		},
		{
			name:        "empty name",
			input:       "",
			wantErr:     true,
			errContains: "cannot be empty",
		},
		{
			name:        "name too long",
			input:       strings.Repeat("a", 65),
			wantErr:     true,
			errContains: "too long",
		},
		{
			name:        "name starting with hyphen",
			input:       "-feature",
			wantErr:     true,
			errContains: "cannot start or end with hyphen",
		},
		{
			name:        "name ending with hyphen",
			input:       "feature-",
			wantErr:     true,
			errContains: "cannot start or end with hyphen",
		},
		{
			name:        "name with path traversal",
			input:       "../feature",
			wantErr:     true,
			errContains: "path traversal",
		},
		{
			name:        "name with forward slash",
			input:       "auth/feature",
			wantErr:     true,
			errContains: "path traversal",
		},
		{
			name:        "name with backslash",
			input:       "auth\\feature",
			wantErr:     true,
			errContains: "path traversal",
		},
		{
			name:        "name with special characters",
			input:       "auth_feature",
			wantErr:     true,
			errContains: "alphanumeric",
		},
		{
			name:        "name with spaces",
			input:       "auth feature",
			wantErr:     true,
			errContains: "alphanumeric",
		},
		{
			name:        "name with dot",
			input:       "auth.feature",
			wantErr:     true,
			errContains: "alphanumeric",
		},
		{
			name:    "maximum valid length",
			input:   strings.Repeat("a", 64),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkflowName(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.True(t, errors.Is(err, ErrInvalidWorkflowName))
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateWorkflowType(t *testing.T) {
	tests := []struct {
		name        string
		input       WorkflowType
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid feature type",
			input:   WorkflowTypeFeature,
			wantErr: false,
		},
		{
			name:    "valid fix type",
			input:   WorkflowTypeFix,
			wantErr: false,
		},
		{
			name:        "invalid type",
			input:       WorkflowType("invalid"),
			wantErr:     true,
			errContains: "must be 'feature' or 'fix'",
		},
		{
			name:        "empty type",
			input:       WorkflowType(""),
			wantErr:     true,
			errContains: "must be 'feature' or 'fix'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkflowType(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.True(t, errors.Is(err, ErrInvalidWorkflowType))
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateDescription(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid short description",
			input:   "add authentication",
			wantErr: false,
		},
		{
			name:    "valid long description at boundary",
			input:   strings.Repeat("a", 32768),
			wantErr: false,
		},
		{
			name:    "valid description with special characters",
			input:   "add JWT authentication with @#$%^& symbols",
			wantErr: false,
		},
		{
			name:        "empty description",
			input:       "",
			wantErr:     true,
			errContains: "cannot be empty",
		},
		{
			name:        "description too long",
			input:       strings.Repeat("a", 32769),
			wantErr:     true,
			errContains: "description too long: 32769 characters (max 32768 characters, 1 over limit)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDescription(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateDescriptionWithEnvOverride(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		input       string
		wantErr     bool
		errContains string
	}{
		{
			name:     "env override: valid at custom limit",
			envValue: "1000",
			input:    strings.Repeat("a", 1000),
			wantErr:  false,
		},
		{
			name:        "env override: exceeds custom limit",
			envValue:    "1000",
			input:       strings.Repeat("a", 1001),
			wantErr:     true,
			errContains: "description too long: 1001 characters (max 1000 characters, 1 over limit)",
		},
		{
			name:        "env override: exceeds custom limit by multiple characters",
			envValue:    "500",
			input:       strings.Repeat("a", 550),
			wantErr:     true,
			errContains: "description too long: 550 characters (max 500 characters, 50 over limit)",
		},
		{
			name:     "env override: invalid value falls back to default",
			envValue: "invalid",
			input:    strings.Repeat("a", 32768),
			wantErr:  false,
		},
		{
			name:     "env override: zero value falls back to default",
			envValue: "0",
			input:    strings.Repeat("a", 32768),
			wantErr:  false,
		},
		{
			name:     "env override: negative value falls back to default",
			envValue: "-100",
			input:    strings.Repeat("a", 32768),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvMaxDescriptionLength, tt.envValue)

			err := ValidateDescription(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, tt.errContains, err.Error())
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestNewSkipValidator(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewStateManager(tmpDir)

	tests := []struct {
		name string
	}{
		{
			name: "creates skip validator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewSkipValidator(sm)
			assert.NotNil(t, validator)
			assert.Equal(t, sm, validator.stateManager)
		})
	}
}

func TestSkipValidator_ValidateSkip(t *testing.T) {
	tests := []struct {
		name             string
		setupState       func(sm StateManager) *WorkflowState
		targetPhase      Phase
		forceBackward    bool
		externalPlanPath string
		wantErr          bool
		errContains      string
	}{
		{
			name: "planning to planning is valid",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			targetPhase: PhasePlanning,
			wantErr:     false,
		},
		{
			name: "empty target phase returns error",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			targetPhase: "",
			wantErr:     true,
			errContains: "target phase cannot be empty",
		},
		{
			name: "cannot skip to COMPLETED phase",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			targetPhase: PhaseCompleted,
			wantErr:     true,
			errContains: "cannot skip to COMPLETED phase",
		},
		{
			name: "cannot skip to FAILED phase",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			targetPhase: PhaseFailed,
			wantErr:     true,
			errContains: "cannot skip to FAILED phase",
		},
		{
			name: "confirmation requires plan.json",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			targetPhase: PhaseConfirmation,
			wantErr:     true,
			errContains: "plan.json not found",
		},
		{
			name: "confirmation valid with existing plan.json",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				plan := &Plan{Summary: "test plan"}
				sm.SavePlan("test", plan)
				return state
			},
			targetPhase: PhaseConfirmation,
			wantErr:     false,
		},
		{
			name: "confirmation valid with external plan",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			targetPhase:      PhaseConfirmation,
			externalPlanPath: "/path/to/external/plan.json",
			wantErr:          false,
		},
		{
			name: "implementation requires plan and approval",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			targetPhase: PhaseImplementation,
			wantErr:     true,
			errContains: "missing prerequisites",
		},
		{
			name: "implementation valid with plan and approval",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				plan := &Plan{Summary: "test plan"}
				sm.SavePlan("test", plan)
				state.Phases[PhaseConfirmation].Status = StatusCompleted
				return state
			},
			targetPhase: PhaseImplementation,
			wantErr:     false,
		},
		{
			name: "refactoring requires plan, approval, and implementation",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				plan := &Plan{Summary: "test plan"}
				sm.SavePlan("test", plan)
				state.Phases[PhaseConfirmation].Status = StatusCompleted
				return state
			},
			targetPhase: PhaseRefactoring,
			wantErr:     true,
			errContains: "implementation phase not completed",
		},
		{
			name: "refactoring valid with all prerequisites",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				plan := &Plan{Summary: "test plan"}
				sm.SavePlan("test", plan)
				state.Phases[PhaseConfirmation].Status = StatusCompleted
				state.Phases[PhaseImplementation].Status = StatusCompleted
				return state
			},
			targetPhase: PhaseRefactoring,
			wantErr:     false,
		},
		{
			name: "pr-split requires all artifacts including PR",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				plan := &Plan{Summary: "test plan"}
				sm.SavePlan("test", plan)
				state.Phases[PhaseConfirmation].Status = StatusCompleted
				state.Phases[PhaseImplementation].Status = StatusCompleted
				return state
			},
			targetPhase: PhasePRSplit,
			wantErr:     true,
			errContains: "refactoring phase not completed",
		},
		{
			name: "pr-split valid with all prerequisites",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				plan := &Plan{Summary: "test plan"}
				sm.SavePlan("test", plan)
				state.Phases[PhaseConfirmation].Status = StatusCompleted
				state.Phases[PhaseImplementation].Status = StatusCompleted
				state.Phases[PhaseRefactoring].Status = StatusCompleted
				return state
			},
			targetPhase: PhasePRSplit,
			wantErr:     false,
		},
		{
			name: "backward skip without force returns error",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				state.CurrentPhase = PhaseImplementation
				return state
			},
			targetPhase:   PhaseConfirmation,
			forceBackward: false,
			wantErr:       true,
			errContains:   "cannot skip backward",
		},
		{
			name: "backward skip with force is allowed",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				plan := &Plan{Summary: "test plan"}
				sm.SavePlan("test", plan)
				state.CurrentPhase = PhaseImplementation
				return state
			},
			targetPhase:   PhaseConfirmation,
			forceBackward: true,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			sm := NewStateManager(tmpDir)
			validator := NewSkipValidator(sm)

			state := tt.setupState(sm)
			err := validator.ValidateSkip(state, tt.targetPhase, tt.forceBackward, tt.externalPlanPath)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestSkipValidator_validateArtifactPrerequisite(t *testing.T) {
	tests := []struct {
		name             string
		setupState       func(sm StateManager) *WorkflowState
		artifact         ArtifactType
		externalPlanPath string
		wantSatisfied    bool
		wantReason       string
	}{
		{
			name: "plan artifact satisfied with existing file",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				plan := &Plan{Summary: "test plan"}
				sm.SavePlan("test", plan)
				return state
			},
			artifact:      ArtifactPlan,
			wantSatisfied: true,
			wantReason:    "",
		},
		{
			name: "plan artifact satisfied with external path",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			artifact:         ArtifactPlan,
			externalPlanPath: "/path/to/plan.json",
			wantSatisfied:    true,
			wantReason:       "",
		},
		{
			name: "plan artifact not satisfied",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			artifact:      ArtifactPlan,
			wantSatisfied: false,
			wantReason:    "plan.json not found",
		},
		{
			name: "approval artifact satisfied",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				state.Phases[PhaseConfirmation].Status = StatusCompleted
				return state
			},
			artifact:      ArtifactApproval,
			wantSatisfied: true,
			wantReason:    "",
		},
		{
			name: "approval artifact not satisfied",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			artifact:      ArtifactApproval,
			wantSatisfied: false,
			wantReason:    "confirmation phase not completed",
		},
		{
			name: "implementation artifact satisfied",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				state.Phases[PhaseImplementation].Status = StatusCompleted
				return state
			},
			artifact:      ArtifactImplementation,
			wantSatisfied: true,
			wantReason:    "",
		},
		{
			name: "implementation artifact not satisfied",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			artifact:      ArtifactImplementation,
			wantSatisfied: false,
			wantReason:    "implementation phase not completed",
		},
		{
			name: "pr artifact satisfied",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				state.Phases[PhaseRefactoring].Status = StatusCompleted
				return state
			},
			artifact:      ArtifactPR,
			wantSatisfied: true,
			wantReason:    "",
		},
		{
			name: "pr artifact not satisfied",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			artifact:      ArtifactPR,
			wantSatisfied: false,
			wantReason:    "refactoring phase not completed",
		},
		{
			name: "unknown artifact type",
			setupState: func(sm StateManager) *WorkflowState {
				state, _ := sm.InitState("test", "test description", WorkflowTypeFeature)
				return state
			},
			artifact:      ArtifactType("unknown"),
			wantSatisfied: false,
			wantReason:    "unknown artifact type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			sm := NewStateManager(tmpDir)
			validator := NewSkipValidator(sm)

			state := tt.setupState(sm)
			satisfied, reason := validator.validateArtifactPrerequisite(state, tt.artifact, tt.externalPlanPath)

			assert.Equal(t, tt.wantSatisfied, satisfied)
			if !tt.wantSatisfied {
				assert.Contains(t, reason, tt.wantReason)
			}
		})
	}
}

func TestGetPhaseOrder(t *testing.T) {
	tests := []struct {
		name  string
		phase Phase
		want  int
	}{
		{
			name:  "planning is 0",
			phase: PhasePlanning,
			want:  0,
		},
		{
			name:  "confirmation is 1",
			phase: PhaseConfirmation,
			want:  1,
		},
		{
			name:  "implementation is 2",
			phase: PhaseImplementation,
			want:  2,
		},
		{
			name:  "refactoring is 3",
			phase: PhaseRefactoring,
			want:  3,
		},
		{
			name:  "pr-split is 4",
			phase: PhasePRSplit,
			want:  4,
		},
		{
			name:  "completed is 5",
			phase: PhaseCompleted,
			want:  5,
		},
		{
			name:  "failed is -1",
			phase: PhaseFailed,
			want:  -1,
		},
		{
			name:  "unknown is -1",
			phase: Phase("unknown"),
			want:  -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPhaseOrder(tt.phase)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCalculateSkippedPhases(t *testing.T) {
	tests := []struct {
		name    string
		current Phase
		target  Phase
		want    []Phase
	}{
		{
			name:    "planning to implementation skips confirmation",
			current: PhasePlanning,
			target:  PhaseImplementation,
			want:    []Phase{PhaseConfirmation},
		},
		{
			name:    "planning to refactoring skips confirmation and implementation",
			current: PhasePlanning,
			target:  PhaseRefactoring,
			want:    []Phase{PhaseConfirmation, PhaseImplementation},
		},
		{
			name:    "confirmation to pr-split skips implementation and refactoring",
			current: PhaseConfirmation,
			target:  PhasePRSplit,
			want:    []Phase{PhaseImplementation, PhaseRefactoring},
		},
		{
			name:    "adjacent phases skip nothing",
			current: PhasePlanning,
			target:  PhaseConfirmation,
			want:    nil,
		},
		{
			name:    "same phase skips nothing",
			current: PhasePlanning,
			target:  PhasePlanning,
			want:    nil,
		},
		{
			name:    "backward skip returns empty",
			current: PhaseImplementation,
			target:  PhasePlanning,
			want:    nil,
		},
		{
			name:    "failed phase returns empty",
			current: PhaseFailed,
			target:  PhasePlanning,
			want:    nil,
		},
		{
			name:    "to failed phase returns empty",
			current: PhasePlanning,
			target:  PhaseFailed,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateSkippedPhases(tt.current, tt.target)
			assert.Equal(t, tt.want, got)
		})
	}
}
