package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// validWorkflowNameRegex ensures alphanumeric characters and hyphens only
	validWorkflowNameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)
)

// ValidateWorkflowName validates a workflow name
// Rules:
// - 1-64 characters
// - Alphanumeric and hyphens only
// - Cannot start or end with hyphen
// - No path traversal sequences
func ValidateWorkflowName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalidWorkflowName)
	}

	if len(name) > 64 {
		return fmt.Errorf("%w: name too long (max 64 characters)", ErrInvalidWorkflowName)
	}

	// Check for path traversal first (more specific error message)
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("%w: name cannot contain path traversal sequences", ErrInvalidWorkflowName)
	}

	if !validWorkflowNameRegex.MatchString(name) {
		return fmt.Errorf("%w: must contain only alphanumeric characters and hyphens, and cannot start or end with hyphen", ErrInvalidWorkflowName)
	}

	return nil
}

// ValidateWorkflowType validates a workflow type
func ValidateWorkflowType(wfType WorkflowType) error {
	if wfType != WorkflowTypeFeature && wfType != WorkflowTypeFix {
		return fmt.Errorf("%w: must be 'feature' or 'fix'", ErrInvalidWorkflowType)
	}
	return nil
}

// ValidateDescription validates a workflow description
// Rules:
// - Minimum MinDescriptionLength characters
// - Maximum configurable length (default DefaultMaxDescriptionLength, override via CLAUDE_WORKFLOW_MAX_DESCRIPTION_LENGTH)
// - Cannot be empty
func ValidateDescription(desc string) error {
	if desc == "" {
		return fmt.Errorf("description cannot be empty")
	}

	maxLength := GetMaxDescriptionLength()
	if len(desc) > maxLength {
		overLimit := len(desc) - maxLength
		return fmt.Errorf("description too long: %d characters (max %d characters, %d over limit)", len(desc), maxLength, overLimit)
	}

	return nil
}

// SkipValidator validates whether a skip to a target phase is allowed
type SkipValidator struct {
	stateManager StateManager
	baseDir      string
}

// NewSkipValidator creates a new SkipValidator
func NewSkipValidator(stateManager StateManager, baseDir string) *SkipValidator {
	return &SkipValidator{
		stateManager: stateManager,
		baseDir:      baseDir,
	}
}

// ValidateSkip validates whether a skip to the target phase is allowed
// Returns nil if valid, or an error describing what's missing/wrong
func (v *SkipValidator) ValidateSkip(state *WorkflowState, targetPhase Phase, forceBackward bool, externalPlanPath string) error {
	if targetPhase == "" {
		return fmt.Errorf("target phase cannot be empty")
	}

	if targetPhase == PhaseCompleted {
		return fmt.Errorf("cannot skip to COMPLETED phase: workflow must complete naturally")
	}

	if targetPhase == PhaseFailed {
		return fmt.Errorf("cannot skip to FAILED phase: this is an error state")
	}

	currentPhaseOrder := getPhaseOrder(state.CurrentPhase)
	targetPhaseOrder := getPhaseOrder(targetPhase)

	if targetPhaseOrder < currentPhaseOrder {
		if !forceBackward {
			return fmt.Errorf("cannot skip backward from %s to %s (use --force-backward to override)", state.CurrentPhase, targetPhase)
		}
	}

	prereqs, exists := PhasePrerequisitesMap[targetPhase]
	if !exists {
		return fmt.Errorf("no prerequisites defined for phase %s", targetPhase)
	}

	var missingPrereqs []string
	for _, prereq := range prereqs.Prerequisites {
		satisfied, reason := v.checkArtifact(state, prereq.ArtifactType, externalPlanPath)
		if !satisfied {
			missingPrereqs = append(missingPrereqs, reason)
		}
	}

	if len(missingPrereqs) > 0 {
		return fmt.Errorf("cannot skip to %s: missing prerequisites:\n  - %s", targetPhase, strings.Join(missingPrereqs, "\n  - "))
	}

	return nil
}

// checkArtifact checks if a specific artifact requirement is satisfied
func (v *SkipValidator) checkArtifact(state *WorkflowState, artifact ArtifactType, externalPlanPath string) (bool, string) {
	switch artifact {
	case ArtifactPlan:
		planPath := filepath.Join(v.stateManager.WorkflowDir(state.Name), planFileName)
		if _, err := os.Stat(planPath); err == nil {
			return true, ""
		}
		if externalPlanPath != "" {
			return true, ""
		}
		return false, "plan.json not found (use --with-plan to provide an external plan)"

	case ArtifactApproval:
		if phaseState, ok := state.Phases[PhaseConfirmation]; ok {
			if phaseState.Status == StatusCompleted {
				return true, ""
			}
		}
		return false, "confirmation phase not completed (the plan must be approved first)"

	case ArtifactImplementation:
		if phaseState, ok := state.Phases[PhaseImplementation]; ok {
			if phaseState.Status == StatusCompleted {
				return true, ""
			}
		}
		return false, "implementation phase not completed"

	case ArtifactPR:
		if phaseState, ok := state.Phases[PhaseRefactoring]; ok {
			if phaseState.Status == StatusCompleted {
				return true, ""
			}
		}
		return false, "refactoring phase not completed (PR must be created first)"

	default:
		return false, fmt.Sprintf("unknown artifact type: %s", artifact)
	}
}

// getPhaseOrder returns the numeric order of a phase for comparison
func getPhaseOrder(phase Phase) int {
	switch phase {
	case PhasePlanning:
		return 0
	case PhaseConfirmation:
		return 1
	case PhaseImplementation:
		return 2
	case PhaseRefactoring:
		return 3
	case PhasePRSplit:
		return 4
	case PhaseCompleted:
		return 5
	case PhaseFailed:
		return -1
	default:
		return -1
	}
}

// calculateSkippedPhases returns the phases that would be skipped going from current to target
func calculateSkippedPhases(current, target Phase) []Phase {
	currentOrder := getPhaseOrder(current)
	targetOrder := getPhaseOrder(target)

	if currentOrder >= targetOrder || currentOrder < 0 || targetOrder < 0 {
		return nil
	}

	allPhases := []Phase{
		PhasePlanning,
		PhaseConfirmation,
		PhaseImplementation,
		PhaseRefactoring,
		PhasePRSplit,
	}

	var skipped []Phase
	for _, phase := range allPhases {
		phaseOrder := getPhaseOrder(phase)
		if phaseOrder > currentOrder && phaseOrder < targetOrder {
			skipped = append(skipped, phase)
		}
	}

	return skipped
}
