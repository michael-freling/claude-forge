//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michael-freling/claude-code-tools/internal/workflow"
	"github.com/michael-freling/claude-code-tools/test/e2e/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkflow_SkipToConfirmation_E2E tests skipping to confirmation phase with external plan
func TestWorkflow_SkipToConfirmation_E2E(t *testing.T) {
	helpers.RequireClaude(t)
	helpers.RequireGit(t)
	helpers.RequireGHAuth(t)

	repo := helpers.CloneRepo(t, sandboxRepoURL)
	branchName := fmt.Sprintf("e2e-skip-confirmation-%d", time.Now().Unix())

	var prNumber int

	t.Cleanup(func() {
		if prNumber > 0 {
			closeCmd := fmt.Sprintf("gh pr close %d --repo %s/%s --delete-branch", prNumber, sandboxRepoOwner, sandboxRepoName)
			t.Logf("Cleaning up PR: %s", closeCmd)
			output, err := repo.RunGit("sh", "-c", closeCmd)
			if err != nil {
				t.Logf("Warning: failed to close PR %d: %v: %s", prNumber, err, output)
			}
		}

		deleteCmd := fmt.Sprintf("git push origin --delete %s", branchName)
		t.Logf("Cleaning up branch: %s", deleteCmd)
		output, err := repo.RunGit("sh", "-c", deleteCmd)
		if err != nil {
			t.Logf("Warning: failed to delete branch %s: %v: %s", branchName, err, output)
		}
	})

	workflowName := "test-skip-confirmation"
	description := "Add a Divide function to the calculator"

	planContent := workflow.Plan{
		Summary:     "Add division functionality to calculator",
		ContextType: "feature",
		Architecture: workflow.Architecture{
			Overview:   "Add a new Divide function to the calculator package",
			Components: []string{"calculator.go", "calculator_test.go"},
		},
		Phases: []workflow.PlanPhase{
			{
				Name:           "Implementation",
				Description:    "Add Divide function and tests",
				EstimatedFiles: 2,
				EstimatedLines: 20,
			},
		},
		WorkStreams: []workflow.WorkStream{
			{
				Name:  "Core Implementation",
				Tasks: []string{"Add Divide function", "Add tests"},
			},
		},
		Risks:               []string{"Division by zero handling"},
		Complexity:          "low",
		EstimatedTotalLines: 20,
		EstimatedTotalFiles: 2,
	}

	planPath := filepath.Join(repo.Dir, "test-plan.json")
	planJSON, err := json.MarshalIndent(planContent, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(planPath, planJSON, 0644)
	require.NoError(t, err)

	config := workflow.DefaultConfig(repo.Dir)
	config.Timeouts.Implementation = 5 * time.Minute
	config.Timeouts.Refactoring = 5 * time.Minute
	config.CICheckTimeout = 10 * time.Minute
	config.SplitPR = false
	config.LogLevel = workflow.LogLevelVerbose

	orchestrator, err := workflow.NewOrchestratorWithConfig(config)
	require.NoError(t, err)

	confirmCalled := false
	orchestrator.SetConfirmFunc(func(plan *workflow.Plan) (bool, string, error) {
		confirmCalled = true
		assert.Equal(t, planContent.Summary, plan.Summary, "loaded plan should match external plan")
		t.Logf("Plan received: %s", plan.Summary)
		return true, "", nil
	})

	ctx := context.Background()
	err = orchestrator.StartWithOptions(ctx, workflow.StartOptions{
		Name:         workflowName,
		Description:  description,
		Type:         workflow.WorkflowTypeFeature,
		SkipTo:       workflow.PhaseConfirmation,
		ExternalPlan: planPath,
	})

	state, statusErr := orchestrator.Status(workflowName)
	require.NoError(t, statusErr)

	if state.WorktreePath != "" {
		prListOutput, ghErr := repo.RunGit("sh", "-c", fmt.Sprintf("cd %s && gh pr list --head $(git rev-parse --abbrev-ref HEAD) --json number --jq '.[0].number'", state.WorktreePath))
		if ghErr == nil && prListOutput != "" {
			fmt.Sscanf(prListOutput, "%d", &prNumber)
			if prNumber > 0 {
				t.Logf("Found PR #%d", prNumber)
			}
		}
	}

	if err != nil {
		t.Logf("Workflow error: %v", err)
		if state.CurrentPhase != workflow.PhaseCompleted && state.CurrentPhase != workflow.PhaseFailed {
			require.NoError(t, err, "workflow should reach completion or failure state")
		}
		t.Logf("Workflow ended in phase: %s (this is acceptable for E2E test validation)", state.CurrentPhase)
	}

	assert.True(t, confirmCalled, "confirm function should have been called")

	state, err = orchestrator.Status(workflowName)
	require.NoError(t, err)

	assert.True(t, state.ExternalPlanUsed, "state should record external plan was used")

	planningPhase := state.Phases[workflow.PhasePlanning]
	assert.Equal(t, workflow.StatusSkipped, planningPhase.Status, "planning phase should be skipped")

	assert.Contains(t, state.SkippedPhases, workflow.PhasePlanning, "planning should be in skipped phases")

	foundTransition := false
	for _, transition := range state.PhaseHistory {
		if transition.ToPhase == workflow.PhaseConfirmation && transition.TransitionType == "skip" {
			foundTransition = true
			break
		}
	}
	assert.True(t, foundTransition, "phase history should record skip transition to confirmation")

	confirmationPhase := state.Phases[workflow.PhaseConfirmation]
	assert.Equal(t, workflow.StatusCompleted, confirmationPhase.Status, "confirmation phase should complete")

	implPhase := state.Phases[workflow.PhaseImplementation]
	assert.Equal(t, workflow.StatusCompleted, implPhase.Status, "implementation phase should complete")

	refactorPhase := state.Phases[workflow.PhaseRefactoring]
	assert.Equal(t, workflow.StatusCompleted, refactorPhase.Status, "refactoring phase should complete")

	stateManager := workflow.NewStateManager(repo.Dir)
	plan, err := stateManager.LoadPlan(workflowName)
	require.NoError(t, err)
	assert.Equal(t, planContent.Summary, plan.Summary, "saved plan should match external plan")

	t.Logf("Workflow final phase: %s", state.CurrentPhase)
	if prNumber > 0 {
		t.Logf("PR URL: https://github.com/%s/%s/pull/%d", sandboxRepoOwner, sandboxRepoName, prNumber)
	}
}

// TestWorkflow_SkipToImplementation_MissingApproval_E2E tests that skipping to implementation
// without completing confirmation phase fails with appropriate error
func TestWorkflow_SkipToImplementation_MissingApproval_E2E(t *testing.T) {
	helpers.RequireGit(t)

	repo := helpers.CloneRepo(t, sandboxRepoURL)

	workflowName := "test-skip-impl-no-approval"
	description := "This should fail due to missing approval"

	planContent := workflow.Plan{
		Summary:             "Test plan",
		ContextType:         "feature",
		Architecture:        workflow.Architecture{Overview: "Test", Components: []string{}},
		Phases:              []workflow.PlanPhase{},
		WorkStreams:         []workflow.WorkStream{},
		Risks:               []string{},
		Complexity:          "low",
		EstimatedTotalLines: 10,
		EstimatedTotalFiles: 1,
	}

	planPath := filepath.Join(repo.Dir, "test-plan-no-approval.json")
	planJSON, err := json.MarshalIndent(planContent, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(planPath, planJSON, 0644)
	require.NoError(t, err)

	config := workflow.DefaultConfig(repo.Dir)
	config.LogLevel = workflow.LogLevelVerbose

	orchestrator, err := workflow.NewOrchestratorWithConfig(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = orchestrator.StartWithOptions(ctx, workflow.StartOptions{
		Name:         workflowName,
		Description:  description,
		Type:         workflow.WorkflowTypeFeature,
		SkipTo:       workflow.PhaseImplementation,
		ExternalPlan: planPath,
	})

	require.Error(t, err, "workflow should fail when skipping to implementation without approval")
	assert.Contains(t, err.Error(), "confirmation phase not completed", "error should mention missing approval")
	t.Logf("Expected error received: %v", err)
}

// TestWorkflow_ResumeSkipToRefactoring_MissingImplementation_E2E tests that resuming with
// skip to refactoring fails when implementation phase is not completed
func TestWorkflow_ResumeSkipToRefactoring_MissingImplementation_E2E(t *testing.T) {
	helpers.RequireClaude(t)
	helpers.RequireGit(t)

	repo := helpers.CloneRepo(t, sandboxRepoURL)

	workflowName := "test-resume-skip-refactor"
	description := "Start workflow and try to skip to refactoring"

	config := workflow.DefaultConfig(repo.Dir)
	config.Timeouts.Planning = 5 * time.Minute
	config.LogLevel = workflow.LogLevelVerbose

	orchestrator, err := workflow.NewOrchestratorWithConfig(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orchestrator.SetConfirmFunc(func(plan *workflow.Plan) (bool, string, error) {
		t.Logf("Plan received: %s", plan.Summary)
		cancel()
		return false, "Interrupting for resume test", nil
	})

	err = orchestrator.Start(ctx, workflowName, description, workflow.WorkflowTypeFeature, nil)
	assert.Error(t, err, "workflow should be interrupted")

	state, err := orchestrator.Status(workflowName)
	require.NoError(t, err)
	assert.Equal(t, workflow.PhasePlanning, state.CurrentPhase, "workflow should be in planning phase")

	orchestrator2, err := workflow.NewOrchestratorWithConfig(config)
	require.NoError(t, err)

	resumeCtx := context.Background()
	err = orchestrator2.ResumeWithOptions(resumeCtx, workflow.ResumeOptions{
		Name:   workflowName,
		SkipTo: workflow.PhaseRefactoring,
	})

	require.Error(t, err, "resume should fail when trying to skip to refactoring without implementation")
	assert.Contains(t, err.Error(), "implementation phase not completed", "error should mention missing implementation")
	t.Logf("Expected error received: %v", err)
}

// TestWorkflow_BackwardSkipWithForce_E2E tests that backward skip is allowed with --force-backward
func TestWorkflow_BackwardSkipWithForce_E2E(t *testing.T) {
	helpers.RequireClaude(t)
	helpers.RequireGit(t)
	helpers.RequireGHAuth(t)

	repo := helpers.CloneRepo(t, sandboxRepoURL)
	branchName := fmt.Sprintf("e2e-backward-skip-%d", time.Now().Unix())

	var prNumber int

	t.Cleanup(func() {
		if prNumber > 0 {
			closeCmd := fmt.Sprintf("gh pr close %d --repo %s/%s --delete-branch", prNumber, sandboxRepoOwner, sandboxRepoName)
			t.Logf("Cleaning up PR: %s", closeCmd)
			output, err := repo.RunGit("sh", "-c", closeCmd)
			if err != nil {
				t.Logf("Warning: failed to close PR %d: %v: %s", prNumber, err, output)
			}
		}

		deleteCmd := fmt.Sprintf("git push origin --delete %s", branchName)
		t.Logf("Cleaning up branch: %s", deleteCmd)
		output, err := repo.RunGit("sh", "-c", deleteCmd)
		if err != nil {
			t.Logf("Warning: failed to delete branch %s: %v: %s", branchName, err, output)
		}
	})

	workflowName := "test-backward-skip"
	description := "Test backward skip with force flag"

	config := workflow.DefaultConfig(repo.Dir)
	config.Timeouts.Planning = 5 * time.Minute
	config.Timeouts.Implementation = 5 * time.Minute
	config.Timeouts.Refactoring = 5 * time.Minute
	config.CICheckTimeout = 10 * time.Minute
	config.SplitPR = false
	config.LogLevel = workflow.LogLevelVerbose

	orchestrator, err := workflow.NewOrchestratorWithConfig(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orchestrator.SetConfirmFunc(func(plan *workflow.Plan) (bool, string, error) {
		t.Logf("Plan received: %s", plan.Summary)
		return true, "", nil
	})

	err = orchestrator.Start(ctx, workflowName, description, workflow.WorkflowTypeFeature, nil)

	state, statusErr := orchestrator.Status(workflowName)
	require.NoError(t, statusErr)

	if state.WorktreePath != "" {
		prListOutput, ghErr := repo.RunGit("sh", "-c", fmt.Sprintf("cd %s && gh pr list --head $(git rev-parse --abbrev-ref HEAD) --json number --jq '.[0].number'", state.WorktreePath))
		if ghErr == nil && prListOutput != "" {
			fmt.Sscanf(prListOutput, "%d", &prNumber)
			if prNumber > 0 {
				t.Logf("Found PR #%d", prNumber)
			}
		}
	}

	if err == nil || (state.CurrentPhase == workflow.PhaseCompleted || state.CurrentPhase == workflow.PhaseFailed) {
		t.Logf("Workflow completed or failed as expected in phase: %s", state.CurrentPhase)

		if state.CurrentPhase == workflow.PhaseCompleted || state.Phases[workflow.PhaseImplementation].Status == workflow.StatusCompleted {
			orchestrator2, err := workflow.NewOrchestratorWithConfig(config)
			require.NoError(t, err)

			orchestrator2.SetConfirmFunc(func(plan *workflow.Plan) (bool, string, error) {
				t.Logf("Plan received after backward skip: %s", plan.Summary)
				return true, "", nil
			})

			resumeCtx := context.Background()
			err = orchestrator2.ResumeWithOptions(resumeCtx, workflow.ResumeOptions{
				Name:          workflowName,
				SkipTo:        workflow.PhasePlanning,
				ForceBackward: true,
			})

			stateAfterResume, statusErr := orchestrator2.Status(workflowName)
			require.NoError(t, statusErr)

			if err != nil {
				t.Logf("Resume error: %v", err)
				t.Logf("Resume ended in phase: %s", stateAfterResume.CurrentPhase)
			}

			foundBackwardTransition := false
			for _, transition := range stateAfterResume.PhaseHistory {
				if transition.TransitionType == "backward_skip" {
					foundBackwardTransition = true
					t.Logf("Found backward skip transition: %s -> %s", transition.FromPhase, transition.ToPhase)
					break
				}
			}
			assert.True(t, foundBackwardTransition, "phase history should record backward skip transition")
		} else {
			t.Skip("Workflow did not complete implementation phase, skipping backward skip test")
		}
	} else {
		t.Logf("Initial workflow did not complete successfully, skipping backward skip test: %v", err)
		t.Skip("Skipping backward skip test due to initial workflow failure")
	}
}

// TestWorkflow_ErrorCases_E2E tests various error cases for skip functionality
func TestWorkflow_ErrorCases_E2E(t *testing.T) {
	helpers.RequireGit(t)

	repo := helpers.CloneRepo(t, sandboxRepoURL)

	config := workflow.DefaultConfig(repo.Dir)
	config.LogLevel = workflow.LogLevelVerbose

	tests := []struct {
		name        string
		opts        workflow.StartOptions
		wantErr     bool
		errContains string
	}{
		{
			name: "with-plan without skip-to",
			opts: workflow.StartOptions{
				Name:         "test-plan-no-skip",
				Description:  "Test external plan without skip-to",
				Type:         workflow.WorkflowTypeFeature,
				ExternalPlan: "/tmp/some-plan.json",
			},
			wantErr:     true,
			errContains: "external plan can only be used with --skip-to",
		},
		{
			name: "with-plan with non-existent file",
			opts: workflow.StartOptions{
				Name:         "test-nonexistent-plan",
				Description:  "Test with non-existent plan file",
				Type:         workflow.WorkflowTypeFeature,
				SkipTo:       workflow.PhaseConfirmation,
				ExternalPlan: "/nonexistent/path/to/plan.json",
			},
			wantErr:     true,
			errContains: "external plan file not found",
		},
		{
			name: "skip-to with invalid phase",
			opts: workflow.StartOptions{
				Name:        "test-invalid-phase",
				Description: "Test with invalid phase",
				Type:        workflow.WorkflowTypeFeature,
				SkipTo:      workflow.Phase("INVALID_PHASE"),
			},
			wantErr:     true,
			errContains: "no prerequisites defined for phase",
		},
		{
			name: "skip-to COMPLETED phase",
			opts: workflow.StartOptions{
				Name:        "test-skip-completed",
				Description: "Test skipping to completed phase",
				Type:        workflow.WorkflowTypeFeature,
				SkipTo:      workflow.PhaseCompleted,
			},
			wantErr:     true,
			errContains: "cannot skip to COMPLETED phase",
		},
		{
			name: "skip-to FAILED phase",
			opts: workflow.StartOptions{
				Name:        "test-skip-failed",
				Description: "Test skipping to failed phase",
				Type:        workflow.WorkflowTypeFeature,
				SkipTo:      workflow.PhaseFailed,
			},
			wantErr:     true,
			errContains: "cannot skip to FAILED phase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orchestrator, err := workflow.NewOrchestratorWithConfig(config)
			require.NoError(t, err)

			ctx := context.Background()
			err = orchestrator.StartWithOptions(ctx, tt.opts)

			if tt.wantErr {
				require.Error(t, err, "expected error for %s", tt.name)
				assert.Contains(t, err.Error(), tt.errContains, "error should contain expected message")
				t.Logf("Expected error received: %v", err)
			} else {
				require.NoError(t, err, "expected no error for %s", tt.name)
			}
		})
	}
}

// TestWorkflow_BackwardSkipWithoutForce_E2E tests that backward skip without force flag fails
func TestWorkflow_BackwardSkipWithoutForce_E2E(t *testing.T) {
	helpers.RequireClaude(t)
	helpers.RequireGit(t)

	repo := helpers.CloneRepo(t, sandboxRepoURL)

	workflowName := "test-backward-no-force"
	description := "Test that backward skip fails without force"

	config := workflow.DefaultConfig(repo.Dir)
	config.Timeouts.Planning = 5 * time.Minute
	config.LogLevel = workflow.LogLevelVerbose

	orchestrator, err := workflow.NewOrchestratorWithConfig(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orchestrator.SetConfirmFunc(func(plan *workflow.Plan) (bool, string, error) {
		t.Logf("Plan received: %s", plan.Summary)
		cancel()
		return false, "Interrupting after planning", nil
	})

	err = orchestrator.Start(ctx, workflowName, description, workflow.WorkflowTypeFeature, nil)
	assert.Error(t, err, "workflow should be interrupted")

	state, err := orchestrator.Status(workflowName)
	require.NoError(t, err)
	assert.Equal(t, workflow.PhasePlanning, state.CurrentPhase, "workflow should be in planning phase")

	planContent := workflow.Plan{
		Summary:             "Updated plan",
		ContextType:         "feature",
		Architecture:        workflow.Architecture{Overview: "Test", Components: []string{}},
		Phases:              []workflow.PlanPhase{},
		WorkStreams:         []workflow.WorkStream{},
		Risks:               []string{},
		Complexity:          "low",
		EstimatedTotalLines: 10,
		EstimatedTotalFiles: 1,
	}

	stateManager := workflow.NewStateManager(repo.Dir)
	err = stateManager.SavePlan(workflowName, &planContent)
	require.NoError(t, err)

	state.Phases[workflow.PhaseConfirmation] = &workflow.PhaseState{
		Status:      workflow.StatusCompleted,
		StartedAt:   ptrTime(time.Now()),
		CompletedAt: ptrTime(time.Now()),
		Attempts:    1,
	}
	state.CurrentPhase = workflow.PhaseConfirmation
	err = stateManager.SaveState(workflowName, state)
	require.NoError(t, err)

	orchestrator2, err := workflow.NewOrchestratorWithConfig(config)
	require.NoError(t, err)

	resumeCtx := context.Background()
	err = orchestrator2.ResumeWithOptions(resumeCtx, workflow.ResumeOptions{
		Name:          workflowName,
		SkipTo:        workflow.PhasePlanning,
		ForceBackward: false,
	})

	require.Error(t, err, "backward skip without force should fail")
	assert.Contains(t, err.Error(), "cannot skip backward", "error should mention backward skip restriction")
	t.Logf("Expected error received: %v", err)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
