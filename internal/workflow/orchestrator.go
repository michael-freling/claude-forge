package workflow

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/michael-freling/claude-code-tools/internal/command"
)

// Config holds configuration for the orchestrator
type Config struct {
	BaseDir                    string
	SplitPR                    bool
	Timeouts                   PhaseTimeouts
	ClaudePath                 string
	DangerouslySkipPermissions bool
	CICheckInterval            time.Duration
	CICheckTimeout             time.Duration
	GHCommandTimeout           time.Duration
	MaxFixAttempts             int
	LogLevel                   LogLevel
}

// StartOptions holds options for starting a workflow
type StartOptions struct {
	Name          string
	Description   string
	Type          WorkflowType
	UpdatePR      *int
	SkipTo        Phase
	ExternalPlan  string
	ForceBackward bool
}

// ResumeOptions holds options for resuming a workflow
type ResumeOptions struct {
	Name          string
	SkipTo        Phase
	ForceBackward bool
}

// PhaseTimeouts holds timeout durations for each phase
type PhaseTimeouts struct {
	Planning       time.Duration
	Implementation time.Duration
	Refactoring    time.Duration
	PRSplit        time.Duration
}

// DefaultConfig returns default configuration
func DefaultConfig(baseDir string) *Config {
	return &Config{
		BaseDir:                    baseDir,
		SplitPR:                    false,
		ClaudePath:                 "claude",
		DangerouslySkipPermissions: false,
		CICheckInterval:            30 * time.Second,
		CICheckTimeout:             30 * time.Minute,
		GHCommandTimeout:           2 * time.Minute,
		MaxFixAttempts:             10,
		LogLevel:                   LogLevelNormal,
		Timeouts: PhaseTimeouts{
			Planning:       1 * time.Hour,
			Implementation: 6 * time.Hour,
			Refactoring:    6 * time.Hour,
			PRSplit:        1 * time.Hour,
		},
	}
}

// Orchestrator manages workflow execution
type Orchestrator struct {
	stateManager    StateManager
	executor        ClaudeExecutor
	promptGenerator PromptGenerator
	parser          OutputParser
	config          *Config
	confirmFunc     func(plan *Plan) (bool, string, error)
	worktreeManager WorktreeManager
	logger          Logger
	ghRunner        command.GhRunner
	gitRunner       command.GitRunner
	splitManager    PRSplitManager

	// For testing - if nil, creates real checker
	ciCheckerFactory func(workingDir string, checkInterval time.Duration, commandTimeout time.Duration) CIChecker
}

// NewOrchestrator creates orchestrator with default config
func NewOrchestrator(baseDir string) (*Orchestrator, error) {
	return NewOrchestratorWithConfig(DefaultConfig(baseDir))
}

// NewOrchestratorWithConfig creates orchestrator with custom config
func NewOrchestratorWithConfig(config *Config) (*Orchestrator, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if config.BaseDir == "" {
		return nil, fmt.Errorf("baseDir cannot be empty")
	}

	promptGen, err := NewPromptGenerator()
	if err != nil {
		return nil, fmt.Errorf("failed to create prompt generator: %w", err)
	}

	logger := NewLogger(config.LogLevel)
	executor := NewClaudeExecutorWithPath(config.ClaudePath, logger)
	stateManager := NewStateManager(config.BaseDir)
	parser := NewOutputParser()
	worktreeManager := NewWorktreeManager(config.BaseDir)
	cmdRunner := command.NewRunner()
	ghRunner := command.NewGhRunner(cmdRunner)
	gitRunner := command.NewGitRunner(cmdRunner)
	splitManager := NewPRSplitManager(gitRunner, ghRunner)

	return &Orchestrator{
		stateManager:    stateManager,
		executor:        executor,
		promptGenerator: promptGen,
		parser:          parser,
		config:          config,
		confirmFunc:     defaultConfirmFunc,
		worktreeManager: worktreeManager,
		logger:          logger,
		ghRunner:        ghRunner,
		gitRunner:       gitRunner,
		splitManager:    splitManager,
	}, nil
}

// SetConfirmFunc allows setting a custom confirmation function for testing
func (o *Orchestrator) SetConfirmFunc(fn func(plan *Plan) (bool, string, error)) {
	o.confirmFunc = fn
}

// handleSkipToPhase validates and performs a skip to a target phase
func (o *Orchestrator) handleSkipToPhase(state *WorkflowState, targetPhase Phase, forceBackward bool, externalPlanPath string) error {
	validator := NewSkipValidator(o.stateManager, o.config.BaseDir)
	if err := validator.ValidateSkip(state, targetPhase, forceBackward, externalPlanPath); err != nil {
		return err
	}

	if externalPlanPath != "" {
		plan, err := LoadExternalPlan(externalPlanPath)
		if err != nil {
			return fmt.Errorf("failed to load external plan: %w", err)
		}

		if err := o.stateManager.SavePlan(state.Name, plan); err != nil {
			return fmt.Errorf("failed to save external plan: %w", err)
		}

		planMarkdown := FormatPlanSummary(plan)
		if err := o.stateManager.SavePlanMarkdown(state.Name, planMarkdown); err != nil {
			return fmt.Errorf("failed to save plan markdown: %w", err)
		}

		state.ExternalPlanUsed = true
	}

	skippedPhases := calculateSkippedPhases(state.CurrentPhase, targetPhase)
	MarkPhasesSkipped(state, skippedPhases)

	reason := fmt.Sprintf("Skipped from %s to %s", state.CurrentPhase, targetPhase)
	if externalPlanPath != "" {
		reason += " with external plan"
	}
	RecordPhaseTransition(state, state.CurrentPhase, targetPhase, "skip", reason)

	state.CurrentPhase = targetPhase
	if phaseState, ok := state.Phases[targetPhase]; ok {
		phaseState.Status = StatusInProgress
	}

	return nil
}

// StartWithOptions initializes and runs a new workflow with options
func (o *Orchestrator) StartWithOptions(ctx context.Context, opts StartOptions) error {
	// Validate PR for update if updatePR is provided
	if opts.UpdatePR != nil {
		prManager := NewPRManagerWithRunners(o.config.BaseDir, o.gitRunner, o.ghRunner)
		validationResult, err := prManager.ValidatePRForUpdate(ctx, *opts.UpdatePR)
		if err != nil {
			return fmt.Errorf("failed to validate PR #%d for update: %w", *opts.UpdatePR, err)
		}
		o.logger.Verbose("PR #%d validated for update: branch=%s, state=%s, mergeable=%s",
			validationResult.Number, validationResult.HeadRefName, validationResult.State, validationResult.Mergeable)
	}

	// Check if a workflow with this name already exists
	if o.stateManager.WorkflowExists(opts.Name) {
		existingState, err := o.stateManager.LoadState(opts.Name)
		if err == nil && existingState.CurrentPhase == PhaseFailed {
			// Delete failed workflow to allow restart with same name
			if err := o.stateManager.DeleteWorkflow(opts.Name); err != nil {
				return fmt.Errorf("failed to delete failed workflow: %w", err)
			}
		}
		// If not failed or couldn't load state, InitState will handle the error
	}

	state, err := o.stateManager.InitState(opts.Name, opts.Description, opts.Type)
	if err != nil {
		return fmt.Errorf("failed to initialize workflow: %w", err)
	}

	state.SplitPR = o.config.SplitPR

	// Store update PR information if provided
	if opts.UpdatePR != nil {
		prManager := NewPRManagerWithRunners(o.config.BaseDir, o.gitRunner, o.ghRunner)
		validationResult, err := prManager.ValidatePRForUpdate(ctx, *opts.UpdatePR)
		if err != nil {
			return fmt.Errorf("failed to validate PR #%d for update: %w", *opts.UpdatePR, err)
		}
		state.UpdatePR = opts.UpdatePR
		state.UpdatePRBranch = validationResult.HeadRefName
	}

	// Handle skip if requested
	if opts.SkipTo != "" {
		if err := o.handleSkipToPhase(state, opts.SkipTo, opts.ForceBackward, opts.ExternalPlan); err != nil {
			return fmt.Errorf("failed to skip to phase: %w", err)
		}
	}

	if err := o.stateManager.SaveState(opts.Name, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return o.runWorkflow(ctx, state)
}

// Start initializes and runs a new workflow (backward compatibility wrapper)
func (o *Orchestrator) Start(ctx context.Context, name, description string, wfType WorkflowType, updatePR *int) error {
	return o.StartWithOptions(ctx, StartOptions{
		Name:        name,
		Description: description,
		Type:        wfType,
		UpdatePR:    updatePR,
	})
}

// ResumeWithOptions continues an existing workflow with options
func (o *Orchestrator) ResumeWithOptions(ctx context.Context, opts ResumeOptions) error {
	o.logger.Verbose("Loading workflow state for '%s'", opts.Name)
	state, err := o.stateManager.LoadState(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to load workflow state: %w", err)
	}

	// Re-validate PR if in update mode
	if state.UpdatePR != nil {
		prManager := NewPRManagerWithRunners(o.config.BaseDir, o.gitRunner, o.ghRunner)
		validationResult, err := prManager.ValidatePRForUpdate(ctx, *state.UpdatePR)
		if err != nil {
			return fmt.Errorf("failed to validate PR #%d for update on resume: %w", *state.UpdatePR, err)
		}
		o.logger.Verbose("PR #%d re-validated on resume: branch=%s, state=%s, mergeable=%s",
			validationResult.Number, validationResult.HeadRefName, validationResult.State, validationResult.Mergeable)
	}

	if phaseState, ok := state.Phases[state.CurrentPhase]; ok {
		o.logger.Verbose("Current phase: %s, Status: %s", getPhaseName(state.CurrentPhase), phaseState.Status)
	} else {
		o.logger.Verbose("Current phase: %s", getPhaseName(state.CurrentPhase))
	}

	if state.CurrentPhase == PhaseCompleted {
		return fmt.Errorf("workflow is already completed")
	}

	if state.Error != nil && !state.Error.Recoverable {
		return fmt.Errorf("workflow is in non-recoverable error state: %w", state.Error)
	}

	// If workflow is in FAILED state, restore it to the phase that failed
	if state.CurrentPhase == PhaseFailed {
		if state.Error != nil {
			state.CurrentPhase = state.Error.Phase
		} else {
			// Find the phase that was in progress or failed
			for phase, phaseState := range state.Phases {
				if phaseState.Status == StatusFailed || phaseState.Status == StatusInProgress {
					state.CurrentPhase = phase
					break
				}
			}
		}
		// Reset the phase status to allow retry
		if phaseState, ok := state.Phases[state.CurrentPhase]; ok {
			phaseState.Status = StatusInProgress
		}
	}

	// Preserve CI failure type for phase execution to handle appropriately
	// (the error message is stored in phase feedback, so we only need to preserve the type)
	isCIFailure := state.Error != nil && state.Error.FailureType == FailureTypeCI
	if state.Error != nil && !isCIFailure {
		state.Error = nil
	}

	// Handle skip if requested
	if opts.SkipTo != "" {
		if err := o.handleSkipToPhase(state, opts.SkipTo, opts.ForceBackward, ""); err != nil {
			return fmt.Errorf("failed to skip to phase: %w", err)
		}
	}

	if err := o.stateManager.SaveState(opts.Name, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return o.runWorkflow(ctx, state)
}

// Resume continues an existing workflow from current phase (backward compatibility wrapper)
func (o *Orchestrator) Resume(ctx context.Context, name string) error {
	return o.ResumeWithOptions(ctx, ResumeOptions{
		Name: name,
	})
}

// Status returns current workflow state
func (o *Orchestrator) Status(name string) (*WorkflowState, error) {
	return o.stateManager.LoadState(name)
}

// List returns all workflows with metadata
func (o *Orchestrator) List() ([]WorkflowInfo, error) {
	return o.stateManager.ListWorkflows()
}

// Delete removes a workflow and all its state
func (o *Orchestrator) Delete(name string) error {
	return o.stateManager.DeleteWorkflow(name)
}

// Clean removes all completed workflows
func (o *Orchestrator) Clean() ([]string, error) {
	workflows, err := o.stateManager.ListWorkflows()
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	var deleted []string
	for _, wf := range workflows {
		if wf.Status == "completed" {
			if err := o.stateManager.DeleteWorkflow(wf.Name); err != nil {
				continue
			}
			deleted = append(deleted, wf.Name)
		}
	}

	return deleted, nil
}

// runWorkflow executes the workflow state machine
func (o *Orchestrator) runWorkflow(ctx context.Context, state *WorkflowState) error {
	fmt.Println(Bold(Cyan("Claude Workflow Orchestrator")))
	fmt.Println(strings.Repeat("=", 30))
	fmt.Printf("\n%s: %s\n", Bold("Workflow"), state.Name)
	fmt.Printf("%s: %s\n", Bold("Type"), state.Type)
	fmt.Printf("%s: %s\n", Bold("Description"), state.Description)

	// Log configuration details in verbose mode
	o.logger.Verbose("Configuration:")
	o.logger.Verbose("  Base directory: %s", o.config.BaseDir)
	o.logger.Verbose("  Claude path: %s", o.config.ClaudePath)
	o.logger.Verbose("  Split PR enabled: %v", o.config.SplitPR)
	o.logger.Verbose("  Timeouts: planning=%s, impl=%s, refactor=%s, pr-split=%s",
		FormatDuration(o.config.Timeouts.Planning),
		FormatDuration(o.config.Timeouts.Implementation),
		FormatDuration(o.config.Timeouts.Refactoring),
		FormatDuration(o.config.Timeouts.PRSplit))

	for {
		if state.CurrentPhase == PhaseCompleted || state.CurrentPhase == PhaseFailed {
			if state.CurrentPhase == PhaseCompleted {
				elapsed := time.Since(state.CreatedAt)
				fmt.Printf("\n%s Workflow completed in %s\n", Green("✓"), FormatDuration(elapsed))
			}
			return nil
		}

		if err := o.executePhase(ctx, state); err != nil {
			return err
		}

		if state.CurrentPhase == PhaseCompleted || state.CurrentPhase == PhaseFailed {
			if state.CurrentPhase == PhaseCompleted {
				elapsed := time.Since(state.CreatedAt)
				fmt.Printf("\n%s Workflow completed in %s\n", Green("✓"), FormatDuration(elapsed))
			}
			return nil
		}
	}
}

// executePhase executes the current phase and transitions to the next
func (o *Orchestrator) executePhase(ctx context.Context, state *WorkflowState) error {
	switch state.CurrentPhase {
	case PhasePlanning:
		return o.executePlanning(ctx, state)
	case PhaseConfirmation:
		return o.executeConfirmation(ctx, state)
	case PhaseImplementation:
		return o.executeImplementation(ctx, state)
	case PhaseRefactoring:
		return o.executeRefactoring(ctx, state)
	case PhasePRSplit:
		return o.executePRSplit(ctx, state)
	default:
		return o.failWorkflow(state, fmt.Errorf("%w: %s", ErrInvalidPhase, state.CurrentPhase))
	}
}

// executePlanning runs the planning phase
func (o *Orchestrator) executePlanning(ctx context.Context, state *WorkflowState) error {
	fmt.Printf("\n%s\n", Bold(FormatPhase(PhasePlanning, 5)))
	fmt.Println(strings.Repeat("-", len(FormatPhase(PhasePlanning, 5))))

	phaseState := state.Phases[PhasePlanning]
	phaseState.Attempts++
	now := time.Now()
	phaseState.StartedAt = &now

	if o.logger.IsVerbose() {
		o.logger.Verbose("Saving workflow state to %s", o.stateManager.WorkflowDir(state.Name))
	}
	if err := o.stateManager.SaveState(state.Name, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	prompt, err := o.promptGenerator.GeneratePlanningPrompt(state.Type, state.Description, phaseState.Feedback)
	if err != nil {
		return o.failWorkflow(state, fmt.Errorf("failed to generate planning prompt: %w", err))
	}

	o.logger.Verbose("Generated planning prompt (%d characters)", len(prompt))

	spinner := NewStreamingSpinnerWithLogger("Analyzing codebase...", o.logger)
	spinner.Start()

	result, err := o.executor.ExecuteStreaming(ctx, ExecuteConfig{
		Prompt:                     prompt,
		Timeout:                    o.config.Timeouts.Planning,
		JSONSchema:                 PlanSchema,
		DangerouslySkipPermissions: o.config.DangerouslySkipPermissions,
		Phase:                      string(PhasePlanning),
		Attempt:                    phaseState.Attempts,
		StateManager:               o.stateManager,
		WorkflowName:               state.Name,
	}, spinner.OnProgress)

	if err != nil {
		spinner.Fail("Planning failed")
		if errors.Is(err, ErrPromptTooLong) {
			phaseState.Feedback = append(phaseState.Feedback, "Your previous response was too long and exceeded the prompt limit. Please provide a more concise response with less verbose output.")
			if err := o.stateManager.SaveState(state.Name, state); err != nil {
				return fmt.Errorf("failed to save state: %w", err)
			}
			return nil
		}
		return o.failWorkflow(state, fmt.Errorf("failed to execute planning: %w", err))
	}

	jsonStr, err := o.parser.ExtractJSON(result.Output)
	if err != nil {
		spinner.Fail("Failed to parse planning output")
		// Save raw output for debugging
		if saveErr := o.stateManager.SaveRawOutput(state.Name, PhasePlanning, result.Output); saveErr != nil {
			fmt.Printf("%s Failed to save raw output: %v\n", Yellow("⚠"), saveErr)
		} else {
			fmt.Printf("%s Raw output saved to: %s/phases/planning_raw.txt\n", Yellow("Debug:"), o.stateManager.WorkflowDir(state.Name))
		}
		return o.failWorkflow(state, fmt.Errorf("failed to extract JSON from planning output: %w", err))
	}

	plan, err := o.parser.ParsePlan(jsonStr)
	if err != nil {
		spinner.Fail("Failed to parse plan")
		// Save raw output for debugging
		if saveErr := o.stateManager.SaveRawOutput(state.Name, PhasePlanning, result.Output); saveErr != nil {
			fmt.Printf("%s Failed to save raw output: %v\n", Yellow("⚠"), saveErr)
		} else {
			fmt.Printf("%s Raw output saved to: %s/phases/planning_raw.txt\n", Yellow("Debug:"), o.stateManager.WorkflowDir(state.Name))
		}
		return o.failWorkflow(state, fmt.Errorf("failed to parse plan: %w", err))
	}

	if o.logger.IsVerbose() {
		o.logger.Verbose("Saving plan to %s/plan.json", o.stateManager.WorkflowDir(state.Name))
	}
	if err := o.stateManager.SavePlan(state.Name, plan); err != nil {
		spinner.Fail("Failed to save plan")
		return o.failWorkflow(state, fmt.Errorf("failed to save plan: %w", err))
	}

	// Save plan as markdown for easy viewing
	planMarkdown := FormatPlanSummary(plan)
	if err := o.stateManager.SavePlanMarkdown(state.Name, planMarkdown); err != nil {
		spinner.Fail("Failed to save plan markdown")
		return o.failWorkflow(state, fmt.Errorf("failed to save plan markdown: %w", err))
	}

	if err := o.stateManager.SavePhaseOutput(state.Name, PhasePlanning, plan); err != nil {
		spinner.Fail("Failed to save planning output")
		return o.failWorkflow(state, fmt.Errorf("failed to save planning output: %w", err))
	}

	spinner.Success("Plan created")

	return o.transitionPhase(state, PhaseConfirmation)
}

// executeConfirmation runs the confirmation phase
func (o *Orchestrator) executeConfirmation(ctx context.Context, state *WorkflowState) error {
	fmt.Printf("\n%s\n", Bold(FormatPhase(PhaseConfirmation, 5)))
	fmt.Println(strings.Repeat("-", len(FormatPhase(PhaseConfirmation, 5))))

	phaseState := state.Phases[PhaseConfirmation]
	phaseState.Attempts++
	now := time.Now()
	phaseState.StartedAt = &now

	if err := o.stateManager.SaveState(state.Name, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	plan, err := o.stateManager.LoadPlan(state.Name)
	if err != nil {
		return o.failWorkflow(state, fmt.Errorf("failed to load plan: %w", err))
	}

	approved, feedback, err := o.confirmFunc(plan)
	if err != nil {
		return o.failWorkflow(state, fmt.Errorf("confirmation failed: %w", err))
	}

	if !approved {
		planningPhase := state.Phases[PhasePlanning]
		planningPhase.Feedback = append(planningPhase.Feedback, feedback)
		planningPhase.Status = StatusPending
		return o.transitionPhase(state, PhasePlanning)
	}

	return o.transitionPhase(state, PhaseImplementation)
}

// executeImplementation runs the implementation phase with error-fixing loop
func (o *Orchestrator) executeImplementation(ctx context.Context, state *WorkflowState) error {
	fmt.Printf("\n%s\n", Bold(FormatPhase(PhaseImplementation, 5)))
	fmt.Println(strings.Repeat("-", len(FormatPhase(PhaseImplementation, 5))))

	phaseState := state.Phases[PhaseImplementation]
	now := time.Now()
	phaseState.StartedAt = &now

	if err := o.stateManager.SaveState(state.Name, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	if state.WorktreePath == "" {
		var worktreePath string
		var err error

		if state.UpdatePR != nil && state.UpdatePRBranch != "" {
			o.logger.Verbose("Creating git worktree from existing branch '%s' for workflow '%s'", state.UpdatePRBranch, state.Name)
			worktreePath, err = o.worktreeManager.CreateWorktreeFromExistingBranch(ctx, state.Name, state.UpdatePRBranch)
			if err != nil {
				return o.failWorkflow(state, fmt.Errorf("failed to create worktree from existing branch: %w", err))
			}
			o.logger.Verbose("Worktree created from existing branch at: %s", worktreePath)
			fmt.Printf("%s Created worktree from existing branch '%s' at: %s\n", Green("✓"), state.UpdatePRBranch, worktreePath)
		} else {
			o.logger.Verbose("Creating git worktree for workflow '%s'", state.Name)
			worktreePath, err = o.worktreeManager.CreateWorktree(state.Name)
			if err != nil {
				return o.failWorkflow(state, fmt.Errorf("failed to create worktree: %w", err))
			}
			o.logger.Verbose("Worktree created at: %s", worktreePath)
			fmt.Printf("%s Created worktree at: %s\n", Green("✓"), worktreePath)
		}

		state.WorktreePath = worktreePath
		if err := o.stateManager.SaveState(state.Name, state); err != nil {
			return fmt.Errorf("failed to save state with worktree path: %w", err)
		}
	}

	plan, err := o.stateManager.LoadPlan(state.Name)
	if err != nil {
		return o.failWorkflow(state, fmt.Errorf("failed to load plan: %w", err))
	}

	// Check if we're resuming from a CI failure - if so, skip to CI fix loop
	var lastError string
	startAttempt := 1
	if state.Error != nil && state.Error.FailureType == FailureTypeCI && len(phaseState.Feedback) > 0 {
		// Resuming from CI failure - use the stored error message and start from attempt 2
		lastError = phaseState.Feedback[len(phaseState.Feedback)-1]
		startAttempt = 2
		// Clear the error now that we've extracted the CI failure info
		state.Error = nil
		fmt.Printf("%s Resuming from CI failure, skipping to CI fix...\n", Yellow("⚠"))
	}

	for attempt := startAttempt; attempt <= o.config.MaxFixAttempts; attempt++ {
		phaseState.Attempts = attempt

		var prompt string
		if attempt == 1 {
			prompt, err = o.promptGenerator.GenerateImplementationPrompt(plan)
			if err != nil {
				return o.failWorkflow(state, fmt.Errorf("failed to generate implementation prompt: %w", err))
			}
		} else {
			prompt, err = o.promptGenerator.GenerateFixCIPrompt(lastError)
			if err != nil {
				return o.failWorkflow(state, fmt.Errorf("failed to generate fix prompt: %w", err))
			}
			fmt.Printf("\n%s Attempt %d/%d to fix CI errors\n", Yellow("⚠"), attempt, o.config.MaxFixAttempts)
		}

		spinner := NewStreamingSpinnerWithLogger("Implementing changes...", o.logger)
		spinner.Start()

		result, err := o.executor.ExecuteStreaming(ctx, ExecuteConfig{
			Prompt:                     prompt,
			Timeout:                    o.config.Timeouts.Implementation,
			JSONSchema:                 ImplementationSummarySchema,
			DangerouslySkipPermissions: o.config.DangerouslySkipPermissions,
			WorkingDirectory:           state.WorktreePath,
			Phase:                      string(PhaseImplementation),
			Attempt:                    attempt,
			StateManager:               o.stateManager,
			WorkflowName:               state.Name,
		}, spinner.OnProgress)

		if err != nil {
			spinner.Fail("Implementation failed")
			if errors.Is(err, ErrPromptTooLong) {
				phaseState.Feedback = append(phaseState.Feedback, "Your previous response was too long and exceeded the prompt limit. Please provide a more concise response with less verbose output.")
				if err := o.stateManager.SaveState(state.Name, state); err != nil {
					return fmt.Errorf("failed to save state: %w", err)
				}
				return nil
			}
			return o.failWorkflow(state, fmt.Errorf("failed to execute implementation: %w", err))
		}

		jsonStr, err := o.parser.ExtractJSON(result.Output)
		if err != nil {
			spinner.Fail("Failed to parse implementation output")
			// Save raw output for debugging
			if saveErr := o.stateManager.SaveRawOutput(state.Name, PhaseImplementation, result.Output); saveErr != nil {
				fmt.Printf("%s Failed to save raw output: %v\n", Yellow("⚠"), saveErr)
			} else {
				fmt.Printf("%s Raw output saved to: %s/phases/implementation_raw.txt\n", Yellow("Debug:"), o.stateManager.WorkflowDir(state.Name))
			}
			return o.failWorkflow(state, fmt.Errorf("failed to extract JSON from implementation output: %w", err))
		}

		summary, err := o.parser.ParseImplementationSummary(jsonStr)
		if err != nil {
			spinner.Fail("Failed to parse implementation summary")
			// Save raw output for debugging
			if saveErr := o.stateManager.SaveRawOutput(state.Name, PhaseImplementation, result.Output); saveErr != nil {
				fmt.Printf("%s Failed to save raw output: %v\n", Yellow("⚠"), saveErr)
			} else {
				fmt.Printf("%s Raw output saved to: %s/phases/implementation_raw.txt\n", Yellow("Debug:"), o.stateManager.WorkflowDir(state.Name))
			}
			return o.failWorkflow(state, fmt.Errorf("failed to parse implementation summary: %w", err))
		}

		if err := o.stateManager.SavePhaseOutput(state.Name, PhaseImplementation, summary); err != nil {
			spinner.Fail("Failed to save implementation output")
			return o.failWorkflow(state, fmt.Errorf("failed to save implementation output: %w", err))
		}

		spinner.Success("Implementation complete")

		if err := o.stateManager.SaveState(state.Name, state); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}

		ciSpinner := NewCISpinner("Waiting for CI to complete")
		ciSpinner.Start()

		o.logger.Verbose("Starting CI check with %s interval, %s timeout",
			FormatDuration(o.config.CICheckInterval),
			FormatDuration(o.config.CICheckTimeout))
		workingDir := o.getWorkingDir(state)
		ciChecker := o.getCIChecker(workingDir)
		ciResult, err := ciChecker.WaitForCIWithProgress(ctx, 0, o.config.CICheckTimeout, CheckCIOptions{}, ciSpinner.OnProgress)
		if err != nil {
			ciSpinner.Fail("CI check failed")

			// Handle non-NoPRError case and return early
			if !IsNoPRError(err) {
				phaseState.Feedback = append(phaseState.Feedback, fmt.Sprintf("CI check error: %v", err))
				if saveErr := o.stateManager.SaveState(state.Name, state); saveErr != nil {
					return fmt.Errorf("failed to save state: %w", saveErr)
				}
				return o.failWorkflowCI(state, fmt.Errorf("failed to check CI: %w", err))
			}

			// NoPRError handling - need to create PR first
			ciResult, err = o.handleNoPRError(ctx, state, phaseState, ciChecker)
			if err != nil {
				return o.failWorkflow(state, err)
			}

			// Continue with CI result processing below (don't return here)
		}

		if ciResult.Passed {
			ciSpinner.Success("CI passed")
			return o.transitionPhase(state, PhaseRefactoring)
		}

		ciSpinner.Fail("CI failed")

		// Check if only cancelled jobs - if so, auto-rerun once
		if len(ciResult.CancelledJobs) > 0 && len(ciResult.FailedJobs) == 0 {
			ciResult, err = o.handleCancelledCI(ctx, 0, workingDir, ciResult)
			if err != nil {
				fmt.Printf("%s Failed to rerun cancelled jobs: %v\n", Yellow("⚠"), err)
				// Continue to fix loop with original result
			} else if ciResult.Passed {
				// CI passed after rerun, continue to next phase
				return o.transitionPhase(state, PhaseRefactoring)
			}
			// If still has failures after rerun, continue to fix loop
		}

		lastError = formatCIErrors(ciResult)
		displayCIFailure(ciResult)

		if err := o.stateManager.SaveState(state.Name, state); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}
	}

	return o.failWorkflow(state, fmt.Errorf("exceeded maximum fix attempts (%d)", o.config.MaxFixAttempts))
}

// executeRefactoring runs the refactoring phase with error-fixing loop
func (o *Orchestrator) executeRefactoring(ctx context.Context, state *WorkflowState) error {
	fmt.Printf("\n%s\n", Bold(FormatPhase(PhaseRefactoring, 5)))
	fmt.Println(strings.Repeat("-", len(FormatPhase(PhaseRefactoring, 5))))

	phaseState := state.Phases[PhaseRefactoring]
	now := time.Now()
	phaseState.StartedAt = &now

	if err := o.stateManager.SaveState(state.Name, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	plan, err := o.stateManager.LoadPlan(state.Name)
	if err != nil {
		return o.failWorkflow(state, fmt.Errorf("failed to load plan: %w", err))
	}

	// Check if we're resuming from a CI failure - if so, skip to CI fix loop
	var lastError string
	startAttempt := 1
	if state.Error != nil && state.Error.FailureType == FailureTypeCI && len(phaseState.Feedback) > 0 {
		// Resuming from CI failure - use the stored error message and start from attempt 2
		lastError = phaseState.Feedback[len(phaseState.Feedback)-1]
		startAttempt = 2
		// Clear the error now that we've extracted the CI failure info
		state.Error = nil
		fmt.Printf("%s Resuming from CI failure, skipping to CI fix...\n", Yellow("⚠"))
	}

	for attempt := startAttempt; attempt <= o.config.MaxFixAttempts; attempt++ {
		phaseState.Attempts = attempt

		var prompt string
		if attempt == 1 {
			prompt, err = o.promptGenerator.GenerateRefactoringPrompt(plan)
			if err != nil {
				return o.failWorkflow(state, fmt.Errorf("failed to generate refactoring prompt: %w", err))
			}
		} else {
			prompt, err = o.promptGenerator.GenerateFixCIPrompt(lastError)
			if err != nil {
				return o.failWorkflow(state, fmt.Errorf("failed to generate fix prompt: %w", err))
			}
			fmt.Printf("\n%s Attempt %d/%d to fix CI errors\n", Yellow("⚠"), attempt, o.config.MaxFixAttempts)
		}

		spinner := NewStreamingSpinnerWithLogger("Refactoring code...", o.logger)
		spinner.Start()

		result, err := o.executor.ExecuteStreaming(ctx, ExecuteConfig{
			Prompt:                     prompt,
			Timeout:                    o.config.Timeouts.Refactoring,
			JSONSchema:                 RefactoringSummarySchema,
			DangerouslySkipPermissions: o.config.DangerouslySkipPermissions,
			WorkingDirectory:           state.WorktreePath,
			Phase:                      string(PhaseRefactoring),
			Attempt:                    attempt,
			StateManager:               o.stateManager,
			WorkflowName:               state.Name,
		}, spinner.OnProgress)

		if err != nil {
			spinner.Fail("Refactoring failed")
			if errors.Is(err, ErrPromptTooLong) {
				phaseState.Feedback = append(phaseState.Feedback, "Your previous response was too long and exceeded the prompt limit. Please provide a more concise response with less verbose output.")
				if err := o.stateManager.SaveState(state.Name, state); err != nil {
					return fmt.Errorf("failed to save state: %w", err)
				}
				return nil
			}
			return o.failWorkflow(state, fmt.Errorf("failed to execute refactoring: %w", err))
		}

		jsonStr, err := o.parser.ExtractJSON(result.Output)
		if err != nil {
			spinner.Fail("Failed to parse refactoring output")
			// Save raw output for debugging
			if saveErr := o.stateManager.SaveRawOutput(state.Name, PhaseRefactoring, result.Output); saveErr != nil {
				fmt.Printf("%s Failed to save raw output: %v\n", Yellow("⚠"), saveErr)
			} else {
				fmt.Printf("%s Raw output saved to: %s/phases/refactoring_raw.txt\n", Yellow("Debug:"), o.stateManager.WorkflowDir(state.Name))
			}
			return o.failWorkflow(state, fmt.Errorf("failed to extract JSON from refactoring output: %w", err))
		}

		summary, err := o.parser.ParseRefactoringSummary(jsonStr)
		if err != nil {
			spinner.Fail("Failed to parse refactoring summary")
			// Save raw output for debugging
			if saveErr := o.stateManager.SaveRawOutput(state.Name, PhaseRefactoring, result.Output); saveErr != nil {
				fmt.Printf("%s Failed to save raw output: %v\n", Yellow("⚠"), saveErr)
			} else {
				fmt.Printf("%s Raw output saved to: %s/phases/refactoring_raw.txt\n", Yellow("Debug:"), o.stateManager.WorkflowDir(state.Name))
			}
			return o.failWorkflow(state, fmt.Errorf("failed to parse refactoring summary: %w", err))
		}

		if err := o.stateManager.SavePhaseOutput(state.Name, PhaseRefactoring, summary); err != nil {
			spinner.Fail("Failed to save refactoring output")
			return o.failWorkflow(state, fmt.Errorf("failed to save refactoring output: %w", err))
		}

		spinner.Success("Refactoring complete")

		ciSpinner := NewCISpinner("Waiting for CI to complete")
		ciSpinner.Start()

		workingDir := o.getWorkingDir(state)
		ciChecker := o.getCIChecker(workingDir)
		ciResult, err := ciChecker.WaitForCIWithProgress(ctx, 0, o.config.CICheckTimeout, CheckCIOptions{}, ciSpinner.OnProgress)
		if err != nil {
			ciSpinner.Fail("CI check failed")

			// Handle non-NoPRError case and return early
			if !IsNoPRError(err) {
				phaseState.Feedback = append(phaseState.Feedback, fmt.Sprintf("CI check error: %v", err))
				if saveErr := o.stateManager.SaveState(state.Name, state); saveErr != nil {
					return fmt.Errorf("failed to save state: %w", saveErr)
				}
				return o.failWorkflowCI(state, fmt.Errorf("failed to check CI: %w", err))
			}

			// NoPRError handling - need to create PR first
			ciResult, err = o.handleNoPRError(ctx, state, phaseState, ciChecker)
			if err != nil {
				return o.failWorkflow(state, err)
			}

			// Continue with CI result processing below (don't return here)
		}

		if ciResult.Passed {
			ciSpinner.Success("CI passed")
			break
		}

		ciSpinner.Fail("CI failed")

		// Check if only cancelled jobs - if so, auto-rerun once
		if len(ciResult.CancelledJobs) > 0 && len(ciResult.FailedJobs) == 0 {
			ciResult, err = o.handleCancelledCI(ctx, 0, workingDir, ciResult)
			if err != nil {
				fmt.Printf("%s Failed to rerun cancelled jobs: %v\n", Yellow("⚠"), err)
				// Continue to fix loop with original result
			} else if ciResult.Passed {
				// CI passed after rerun, break from loop
				break
			}
			// If still has failures after rerun, continue to fix loop
		}

		lastError = formatCIErrors(ciResult)
		displayCIFailure(ciResult)

		if err := o.stateManager.SaveState(state.Name, state); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}

		if attempt == o.config.MaxFixAttempts {
			return o.failWorkflow(state, fmt.Errorf("exceeded maximum fix attempts (%d)", o.config.MaxFixAttempts))
		}
	}

	if state.SplitPR {
		// Collect PR metrics for the split prompt
		metrics, err := o.getPRMetrics(ctx, state.WorktreePath)
		if err != nil {
			return o.failWorkflow(state, fmt.Errorf("failed to get PR metrics: %w", err))
		}

		prSplitPhase := state.Phases[PhasePRSplit]
		prSplitPhase.Metrics = metrics

		o.logger.Verbose("PR split enabled, transitioning to PR split phase (lines: %d, files: %d)",
			metrics.LinesChanged, metrics.FilesChanged)

		if err := o.stateManager.SaveState(state.Name, state); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}

		return o.transitionPhase(state, PhasePRSplit)
	}

	return o.transitionPhase(state, PhaseCompleted)
}

// executePRSplit runs the PR split phase with error-checking loop
func (o *Orchestrator) executePRSplit(ctx context.Context, state *WorkflowState) error {
	fmt.Printf("\n%s\n", Bold(FormatPhase(PhasePRSplit, 5)))
	fmt.Println(strings.Repeat("-", len(FormatPhase(PhasePRSplit, 5))))

	phaseState := state.Phases[PhasePRSplit]
	now := time.Now()
	phaseState.StartedAt = &now

	if err := o.stateManager.SaveState(state.Name, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	if phaseState.Metrics == nil {
		return o.failWorkflow(state, fmt.Errorf("PR metrics not available"))
	}

	sourceBranch, err := o.gitRunner.GetCurrentBranch(ctx, state.WorktreePath)
	if err != nil {
		return o.failWorkflow(state, fmt.Errorf("failed to get source branch: %w", err))
	}

	commits, err := o.gitRunner.GetCommits(ctx, state.WorktreePath, "main")
	if err != nil {
		return o.failWorkflow(state, fmt.Errorf("failed to get commits: %w", err))
	}

	var prResult *PRSplitResult
	var lastError string

	for attempt := 1; attempt <= o.config.MaxFixAttempts; attempt++ {
		phaseState.Attempts = attempt

		var prompt string
		if attempt == 1 {
			prompt, err = o.promptGenerator.GeneratePRSplitPrompt(phaseState.Metrics, commits)
			if err != nil {
				return o.failWorkflow(state, fmt.Errorf("failed to generate PR split prompt: %w", err))
			}
		} else {
			prompt, err = o.promptGenerator.GenerateFixCIPrompt(lastError)
			if err != nil {
				return o.failWorkflow(state, fmt.Errorf("failed to generate CI fix prompt: %w", err))
			}
			fmt.Printf("\n%s Attempt %d/%d to fix errors\n", Yellow("⚠"), attempt, o.config.MaxFixAttempts)
		}

		spinner := NewStreamingSpinnerWithLogger("Splitting PR into manageable pieces...", o.logger)
		spinner.Start()

		result, err := o.executor.ExecuteStreaming(ctx, ExecuteConfig{
			Prompt:                     prompt,
			Timeout:                    o.config.Timeouts.PRSplit,
			JSONSchema:                 PRSplitPlanSchema,
			DangerouslySkipPermissions: o.config.DangerouslySkipPermissions,
			WorkingDirectory:           state.WorktreePath,
			Phase:                      string(PhasePRSplit),
			Attempt:                    attempt,
			StateManager:               o.stateManager,
			WorkflowName:               state.Name,
		}, spinner.OnProgress)

		if err != nil {
			spinner.Fail("PR split failed")
			if errors.Is(err, ErrPromptTooLong) {
				phaseState.Feedback = append(phaseState.Feedback, "Your previous response was too long and exceeded the prompt limit. Please provide a more concise response with less verbose output.")
				if err := o.stateManager.SaveState(state.Name, state); err != nil {
					return fmt.Errorf("failed to save state: %w", err)
				}
				return nil
			}
			return o.failWorkflow(state, fmt.Errorf("failed to execute PR split: %w", err))
		}

		jsonStr, err := o.parser.ExtractJSON(result.Output)
		if err != nil {
			spinner.Fail("Failed to parse PR split output")
			if saveErr := o.stateManager.SaveRawOutput(state.Name, PhasePRSplit, result.Output); saveErr != nil {
				fmt.Printf("%s Failed to save raw output: %v\n", Yellow("⚠"), saveErr)
			} else {
				fmt.Printf("%s Raw output saved to: %s/phases/pr_split_raw.txt\n", Yellow("Debug:"), o.stateManager.WorkflowDir(state.Name))
			}
			return o.failWorkflow(state, fmt.Errorf("failed to extract JSON from PR split output: %w", err))
		}

		plan, err := o.parser.ParsePRSplitPlan(jsonStr)
		if err != nil {
			spinner.Fail("Failed to parse PR split plan")
			if saveErr := o.stateManager.SaveRawOutput(state.Name, PhasePRSplit, result.Output); saveErr != nil {
				fmt.Printf("%s Failed to save raw output: %v\n", Yellow("⚠"), saveErr)
			} else {
				fmt.Printf("%s Raw output saved to: %s/phases/pr_split_raw.txt\n", Yellow("Debug:"), o.stateManager.WorkflowDir(state.Name))
			}
			return o.failWorkflow(state, fmt.Errorf("failed to parse PR split plan: %w", err))
		}

		spinner.Success("PR split plan created")

		executionSpinner := NewStreamingSpinnerWithLogger("Creating branches and PRs...", o.logger)
		executionSpinner.Start()

		prResult, err = o.splitManager.ExecuteSplit(ctx, state.WorktreePath, plan, sourceBranch, "main")
		if err != nil {
			executionSpinner.Fail("Failed to create PRs")
			if prResult != nil {
				rollbackErr := o.splitManager.Rollback(ctx, state.WorktreePath, prResult)
				if rollbackErr != nil {
					o.logger.Verbose("Rollback failed: %v", rollbackErr)
				}
			}
			return o.failWorkflow(state, fmt.Errorf("failed to execute PR split: %w", err))
		}

		executionSpinner.Success("PRs created successfully")

		if err := o.stateManager.SavePhaseOutput(state.Name, PhasePRSplit, prResult); err != nil {
			return o.failWorkflow(state, fmt.Errorf("failed to save PR split output: %w", err))
		}

		workingDir := o.getWorkingDir(state)

		allPassed := true
		for i, childPR := range prResult.ChildPRs {
			isLastChild := (i == len(prResult.ChildPRs)-1)

			o.logger.Verbose("Checking child PR #%d: %s", childPR.Number, childPR.Title)
			fmt.Printf("\n%s Checking child PR #%d: %s\n", Bold("→"), childPR.Number, childPR.Title)

			opts := CheckCIOptions{
				SkipE2E: !isLastChild,
			}

			ciSpinner := NewCISpinner("Waiting for CI to complete")
			ciSpinner.Start()

			ciChecker := o.getCIChecker(workingDir)
			ciResult, err := ciChecker.WaitForCIWithProgress(ctx, childPR.Number, o.config.CICheckTimeout, opts, ciSpinner.OnProgress)
			if err != nil {
				ciSpinner.Fail("CI check failed")
				return o.failWorkflowCI(state, fmt.Errorf("failed to check CI on child PR #%d: %w", childPR.Number, err))
			}

			if !ciResult.Passed {
				ciSpinner.Fail("CI failed")

				if len(ciResult.CancelledJobs) > 0 && len(ciResult.FailedJobs) == 0 {
					ciResult, err = o.handleCancelledCI(ctx, childPR.Number, workingDir, ciResult)
					if err != nil {
						fmt.Printf("%s Failed to rerun cancelled jobs for child PR #%d: %v\n", Yellow("⚠"), childPR.Number, err)
					} else if ciResult.Passed {
						ciSpinner.Success("CI passed")
						fmt.Printf("  %s Child PR #%d passed all checks after rerun\n", Green("✓"), childPR.Number)
						continue
					}
				}

				allPassed = false
				if isLastChild {
					fmt.Printf("%s\n", Yellow("Last child PR must pass e2e tests"))
				}
				lastError = formatCIErrors(ciResult)
				displayCIFailure(ciResult)
				break
			}

			ciSpinner.Success("CI passed")
			fmt.Printf("  %s Child PR #%d passed all checks\n", Green("✓"), childPR.Number)
		}

		if allPassed {
			break
		}

		if err := o.stateManager.SaveState(state.Name, state); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}

		if attempt == o.config.MaxFixAttempts {
			return o.failWorkflow(state, fmt.Errorf("exceeded maximum fix attempts (%d)", o.config.MaxFixAttempts))
		}
	}

	return o.transitionPhase(state, PhaseCompleted)
}

// getWorkingDir returns the working directory for the workflow, defaulting to BaseDir if not set
func (o *Orchestrator) getWorkingDir(state *WorkflowState) string {
	if state.WorktreePath != "" {
		return state.WorktreePath
	}
	return o.config.BaseDir
}

// executePRCreation executes the PR creation sub-routine using Claude.
// It attempts to create a PR for the current branch, or finds an existing PR.
// Returns the PR number if created or found, or 0 if PR creation was skipped
// (e.g., no commits on branch). Returns an error if PR creation fails after
// all retry attempts.
func (o *Orchestrator) executePRCreation(ctx context.Context, state *WorkflowState) (int, error) {
	// Skip PR creation if we're in update mode
	if state.UpdatePR != nil {
		o.logger.Verbose("Skipping PR creation - in update mode for PR #%d", *state.UpdatePR)
		return *state.UpdatePR, nil
	}

	o.logger.Verbose("Starting PR creation sub-routine")

	// Get current branch name
	workingDir := o.getWorkingDir(state)

	branch, err := o.gitRunner.GetCurrentBranch(ctx, workingDir)
	if err != nil {
		return 0, fmt.Errorf("failed to get current branch: %w", err)
	}

	// Generate PR creation prompt
	prCtx := &PRCreationContext{
		WorkflowType: state.Type,
		Branch:       branch,
		BaseBranch:   "main", // TODO: make this configurable
		Description:  state.Description,
	}

	prompt, err := o.promptGenerator.GenerateCreatePRPrompt(prCtx)
	if err != nil {
		return 0, fmt.Errorf("failed to generate PR creation prompt: %w", err)
	}

	var lastError string
	for attempt := 1; attempt <= maxPRCreationAttempts; attempt++ {
		if attempt > 1 {
			fmt.Printf("%s Retry %d/%d for PR creation\n", Yellow("⚠"), attempt, maxPRCreationAttempts)
			time.Sleep(prCreationRetryDelay)
		}

		spinner := NewStreamingSpinnerWithLogger("Creating PR...", o.logger)
		spinner.Start()

		result, err := o.executor.ExecuteStreaming(ctx, ExecuteConfig{
			Prompt:                     prompt,
			Timeout:                    prCreationTimeout,
			JSONSchema:                 PRCreationResultSchema,
			DangerouslySkipPermissions: o.config.DangerouslySkipPermissions,
			WorkingDirectory:           workingDir,
			Phase:                      "CREATE_PR",
			Attempt:                    attempt,
			StateManager:               o.stateManager,
			WorkflowName:               state.Name,
		}, spinner.OnProgress)

		if err != nil {
			spinner.Fail("PR creation failed")
			if errors.Is(err, ErrPromptTooLong) {
				o.logger.Verbose("PR creation failed due to prompt too long, skipping retry")
				return 0, fmt.Errorf("PR creation failed: %w", err)
			}
			o.logger.Verbose("PR creation attempt %d failed: %v", attempt, err)
			lastError = err.Error()
			continue
		}

		// Parse the result
		jsonStr, err := o.parser.ExtractJSON(result.Output)
		if err != nil {
			spinner.Fail("Failed to parse PR creation output")
			o.logger.Verbose("Failed to extract JSON from PR creation output: %v", err)
			lastError = fmt.Sprintf("failed to extract JSON: %v", err)
			continue
		}

		var prResult PRCreationResult
		if err := json.Unmarshal([]byte(jsonStr), &prResult); err != nil {
			spinner.Fail("Failed to parse PR creation result")
			o.logger.Verbose("Failed to unmarshal PR creation result: %v", err)
			lastError = fmt.Sprintf("failed to unmarshal PR creation result: %v", err)
			continue
		}

		// Handle successful cases with early returns
		if prResult.Status == "created" || prResult.Status == "exists" {
			statusMsg := "created"
			if prResult.Status == "exists" {
				statusMsg = "already exists"
			}
			spinner.Success(fmt.Sprintf("PR #%d %s", prResult.PRNumber, statusMsg))
			o.logger.Verbose("PR %s: #%d - %s", prResult.Status, prResult.PRNumber, prResult.Message)
			logPRMetadata(prResult.Metadata)
			return prResult.PRNumber, nil
		}

		// Handle skipped case with early return
		if prResult.Status == "skipped" {
			spinner.Success("PR creation skipped")
			o.logger.Verbose("PR creation skipped: %s", prResult.Message)
			fmt.Printf("%s %s\n", Yellow("⚠"), prResult.Message)
			return 0, nil
		}

		// Failed or unknown status - log and continue to retry
		spinner.Fail("PR creation failed")
		o.logger.Verbose("PR creation failed with status %q: %s", prResult.Status, prResult.Message)
		lastError = prResult.Message
	}

	return 0, fmt.Errorf("failed to create PR after %d attempts: %s", maxPRCreationAttempts, lastError)
}

// handleNoPRError handles the case when no PR exists during CI check.
// It attempts to create a PR and retry the CI check. Returns the CI result
// if successful, or an error if PR creation or CI check fails.
func (o *Orchestrator) handleNoPRError(
	ctx context.Context,
	state *WorkflowState,
	phaseState *PhaseState,
	ciChecker CIChecker,
) (*CIResult, error) {
	o.logger.Verbose("No PR found, initiating PR creation")
	fmt.Printf("%s No PR found for current branch, creating PR...\n", Yellow("⚠"))

	prNumber, prErr := o.executePRCreation(ctx, state)
	if prErr != nil {
		phaseState.Feedback = append(phaseState.Feedback, fmt.Sprintf("PR creation failed: %v", prErr))
		if saveErr := o.stateManager.SaveState(state.Name, state); saveErr != nil {
			return nil, fmt.Errorf("failed to save state: %w", saveErr)
		}
		return nil, fmt.Errorf("failed to create PR: %w", prErr)
	}

	if prNumber == 0 {
		// PR creation was skipped (no commits, etc.)
		phaseState.Feedback = append(phaseState.Feedback, "PR creation skipped - no commits on branch")
		if saveErr := o.stateManager.SaveState(state.Name, state); saveErr != nil {
			return nil, fmt.Errorf("failed to save state: %w", saveErr)
		}
		return nil, fmt.Errorf("no commits on branch to create PR")
	}

	// PR created/found - retry CI check with the PR number
	fmt.Printf("%s Retrying CI check with PR #%d\n", Green("✓"), prNumber)

	ciSpinner := NewCISpinner("Waiting for CI to complete")
	ciSpinner.Start()

	ciResult, err := ciChecker.WaitForCIWithProgress(ctx, prNumber, o.config.CICheckTimeout, CheckCIOptions{}, ciSpinner.OnProgress)
	if err != nil {
		ciSpinner.Fail("CI check failed")
		phaseState.Feedback = append(phaseState.Feedback, fmt.Sprintf("CI check error: %v", err))
		if saveErr := o.stateManager.SaveState(state.Name, state); saveErr != nil {
			return nil, fmt.Errorf("failed to save state: %w", saveErr)
		}
		return nil, fmt.Errorf("failed to check CI: %w", err)
	}

	return ciResult, nil
}

// transitionPhase transitions the workflow to the next phase
func (o *Orchestrator) transitionPhase(state *WorkflowState, nextPhase Phase) error {
	currentPhaseState := state.Phases[state.CurrentPhase]
	now := time.Now()
	currentPhaseState.CompletedAt = &now
	currentPhaseState.Status = StatusCompleted

	// Log phase transition
	var duration time.Duration
	if currentPhaseState.StartedAt != nil {
		duration = now.Sub(*currentPhaseState.StartedAt)
	}
	o.logger.Verbose("Transitioning from %s to %s", getPhaseName(state.CurrentPhase), getPhaseName(nextPhase))
	o.logger.Verbose("Phase %s completed in %s (attempts: %d)",
		getPhaseName(state.CurrentPhase),
		FormatDuration(duration),
		currentPhaseState.Attempts)

	state.CurrentPhase = nextPhase

	if nextPhase == PhaseCompleted || nextPhase == PhaseFailed {
		if err := o.stateManager.SaveState(state.Name, state); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}
		return nil
	}

	nextPhaseState := state.Phases[nextPhase]
	nextPhaseState.Status = StatusInProgress

	if err := o.stateManager.SaveState(state.Name, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

// failWorkflow transitions the workflow to failed state with execution failure type
func (o *Orchestrator) failWorkflow(state *WorkflowState, err error) error {
	return o.failWorkflowWithType(state, err, FailureTypeExecution)
}

// failWorkflowCI transitions the workflow to failed state with CI failure type
func (o *Orchestrator) failWorkflowCI(state *WorkflowState, err error) error {
	return o.failWorkflowWithType(state, err, FailureTypeCI)
}

// failWorkflowWithType transitions the workflow to failed state with a specific failure type
func (o *Orchestrator) failWorkflowWithType(state *WorkflowState, err error, failureType FailureType) error {
	state.Error = &WorkflowError{
		Message:     err.Error(),
		Phase:       state.CurrentPhase,
		Timestamp:   time.Now(),
		Recoverable: isRecoverableError(err),
		FailureType: failureType,
	}

	currentPhaseState := state.Phases[state.CurrentPhase]
	currentPhaseState.Status = StatusFailed

	state.CurrentPhase = PhaseFailed

	if saveErr := o.stateManager.SaveState(state.Name, state); saveErr != nil {
		return fmt.Errorf("failed to save failed state: %w (original error: %v)", saveErr, err)
	}

	return err
}

// getPRMetrics collects PR metrics from git diff
func (o *Orchestrator) getPRMetrics(ctx context.Context, workingDir string) (*PRMetrics, error) {
	output, err := o.gitRunner.GetDiffStat(ctx, workingDir, "origin/main")
	if err != nil {
		return nil, fmt.Errorf("failed to get diff stat: %w", err)
	}

	return parseDiffStat(output)
}

// parseDiffStat parses git diff --stat output
func parseDiffStat(output string) (*PRMetrics, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return &PRMetrics{}, nil
	}

	metrics := &PRMetrics{
		FilesAdded:    []string{},
		FilesModified: []string{},
		FilesDeleted:  []string{},
	}

	for i, line := range lines {
		if i == len(lines)-1 {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if filesChanged, err := strconv.Atoi(parts[0]); err == nil {
					metrics.FilesChanged = filesChanged
				}
			}
			if len(parts) >= 4 {
				if linesChanged, err := strconv.Atoi(parts[3]); err == nil {
					metrics.LinesChanged = linesChanged
				}
			}
			continue
		}

		parts := strings.Fields(line)
		if len(parts) > 0 {
			fileName := parts[0]
			if strings.Contains(line, "(new)") {
				metrics.FilesAdded = append(metrics.FilesAdded, fileName)
			} else if strings.Contains(line, "(gone)") {
				metrics.FilesDeleted = append(metrics.FilesDeleted, fileName)
			} else {
				metrics.FilesModified = append(metrics.FilesModified, fileName)
			}
		}
	}

	return metrics, nil
}

// isRecoverableError determines if an error is recoverable
func isRecoverableError(err error) bool {
	if err == nil {
		return false
	}

	switch {
	case strings.Contains(err.Error(), "timeout"):
		return true
	case strings.Contains(err.Error(), "claude execution failed"):
		return true
	case strings.Contains(err.Error(), "failed to parse"):
		// Parse errors are recoverable since Claude's response can vary on retry
		return true
	case strings.Contains(err.Error(), "invalid workflow name"):
		// Invalid input errors are not recoverable
		return false
	case strings.Contains(err.Error(), "invalid"):
		return false
	default:
		return true
	}
}

// defaultConfirmFunc is the default confirmation function that reads from stdin
func defaultConfirmFunc(plan *Plan) (bool, string, error) {
	fmt.Println()
	fmt.Println(FormatPlanSummary(plan))
	fmt.Println()
	fmt.Println(Cyan("Full plan saved to: .claude/workflow/<name>/plan.md"))
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print(Bold("Approve this plan? [y/n]: "))

		if !scanner.Scan() {
			return false, "", fmt.Errorf("failed to read input")
		}

		response := strings.TrimSpace(strings.ToLower(scanner.Text()))

		if response == "" {
			fmt.Println(Yellow("Please enter 'y' to approve or 'n' to provide feedback."))
			continue
		}

		if response == "yes" || response == "y" {
			return true, "", nil
		}

		if response == "no" || response == "n" {
			fmt.Print("Please provide your feedback: ")

			if !scanner.Scan() {
				return false, "", fmt.Errorf("failed to read feedback input")
			}

			feedback := strings.TrimSpace(scanner.Text())
			fmt.Println(Green("✓") + " Feedback received. Replanning with your suggestions...")
			return false, feedback, nil
		}

		// Treat any other non-empty input as feedback
		fmt.Println(Green("✓") + " Feedback received. Replanning with your suggestions...")
		return false, response, nil
	}
}

// formatCIErrors formats CI errors for the fix prompt
func formatCIErrors(result *CIResult) string {
	var builder strings.Builder
	builder.WriteString("CI checks failed with the following errors:\n\n")
	builder.WriteString(result.Output)

	if len(result.FailedJobs) > 0 {
		builder.WriteString("\n\nFailed jobs:\n")
		for _, job := range result.FailedJobs {
			builder.WriteString("- ")
			builder.WriteString(job)
			builder.WriteString("\n")
		}
	}

	if len(result.CancelledJobs) > 0 {
		if len(result.FailedJobs) > 0 {
			builder.WriteString("\nCancelled jobs (infrastructure issue, not code failure):\n")
		} else {
			builder.WriteString("\n\nCancelled jobs (infrastructure issue, not code failure):\n")
		}
		for _, job := range result.CancelledJobs {
			builder.WriteString("- ")
			builder.WriteString(job)
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

// displayCIFailure displays CI failures to the console
func displayCIFailure(ciResult *CIResult) {
	if len(ciResult.FailedJobs) > 0 {
		fmt.Printf("\n%s\n", Red("CI failures detected:"))
		for _, job := range ciResult.FailedJobs {
			fmt.Printf("  %s %s\n", Red("✗"), job)
		}
	}
	if len(ciResult.CancelledJobs) > 0 {
		fmt.Printf("\n%s\n", Yellow("CI jobs cancelled:"))
		for _, job := range ciResult.CancelledJobs {
			fmt.Printf("  %s %s\n", Yellow("⚠"), job)
		}
	}
	fmt.Printf("\n%s\n", ciResult.Output)
}

// handleCancelledCI checks if only cancelled jobs exist and attempts to rerun them
// Returns the new CI result after rerun, or the original result if no rerun needed
func (o *Orchestrator) handleCancelledCI(ctx context.Context, prNumber int, workingDir string, ciResult *CIResult) (*CIResult, error) {
	// Check if only cancelled jobs (no actual failures)
	if len(ciResult.CancelledJobs) == 0 || len(ciResult.FailedJobs) > 0 {
		return ciResult, nil // Not just cancelled, return as-is
	}

	fmt.Printf("\n%s CI jobs were cancelled (not failed). Attempting to rerun...\n", Yellow("⚠"))

	// Get latest run ID
	runID, err := o.ghRunner.GetLatestRunID(ctx, workingDir, prNumber)
	if err != nil {
		return ciResult, fmt.Errorf("failed to get run ID for rerun: %w", err)
	}

	// Rerun cancelled jobs
	if err := o.ghRunner.RunRerun(ctx, workingDir, runID); err != nil {
		return ciResult, fmt.Errorf("failed to rerun cancelled jobs: %w", err)
	}

	fmt.Printf("%s Cancelled jobs rerun triggered. Waiting for CI...\n", Green("✓"))

	// Wait for CI again
	ciSpinner := NewCISpinner("Waiting for CI to complete after rerun")
	ciSpinner.Start()

	ciChecker := o.getCIChecker(workingDir)
	newResult, err := ciChecker.WaitForCIWithProgress(ctx, prNumber, o.config.CICheckTimeout, CheckCIOptions{}, ciSpinner.OnProgress)
	if err != nil {
		ciSpinner.Fail("CI check failed")
		return ciResult, fmt.Errorf("failed to check CI after rerun: %w", err)
	}

	if newResult.Passed {
		ciSpinner.Success("CI passed after rerun")
	} else {
		ciSpinner.Fail("CI failed after rerun")
	}

	return newResult, nil
}

// getCIChecker creates or retrieves a CIChecker for the given working directory
func (o *Orchestrator) getCIChecker(workingDir string) CIChecker {
	if o.ciCheckerFactory != nil {
		return o.ciCheckerFactory(workingDir, o.config.CICheckInterval, o.config.GHCommandTimeout)
	}
	return NewCIChecker(workingDir, o.config.CICheckInterval, o.config.GHCommandTimeout)
}
