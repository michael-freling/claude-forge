package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michael-freling/claude-code-tools/internal/workflow"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd(t *testing.T) {
	cmd := newRootCmd()

	assert.Equal(t, "claude-workflow", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	commandNames := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		commandNames = append(commandNames, c.Name())
	}
	assert.ElementsMatch(t, []string{"start", "list", "status", "resume", "delete", "clean"}, commandNames)

	persistentFlags := cmd.PersistentFlags()
	assert.NotNil(t, persistentFlags.Lookup("base-dir"))
	assert.NotNil(t, persistentFlags.Lookup("split-pr"))
	assert.NotNil(t, persistentFlags.Lookup("claude-path"))
	assert.NotNil(t, persistentFlags.Lookup("dangerously-skip-permissions"))
	assert.NotNil(t, persistentFlags.Lookup("timeout-planning"))
	assert.NotNil(t, persistentFlags.Lookup("timeout-implementation"))
	assert.NotNil(t, persistentFlags.Lookup("timeout-refactoring"))
	assert.NotNil(t, persistentFlags.Lookup("timeout-pr-split"))
	assert.NotNil(t, persistentFlags.Lookup("verbose"))
}

func TestSubcommands(t *testing.T) {
	tests := []struct {
		name         string
		cmdFunc      func() *cobra.Command
		expectedUse  string
		expectedArgs cobra.PositionalArgs
	}{
		{
			name:         "start command",
			cmdFunc:      newStartCmd,
			expectedUse:  "start <name> <description>",
			expectedArgs: cobra.ExactArgs(2),
		},
		{
			name:         "list command",
			cmdFunc:      newListCmd,
			expectedUse:  "list",
			expectedArgs: cobra.NoArgs,
		},
		{
			name:         "status command",
			cmdFunc:      newStatusCmd,
			expectedUse:  "status <name>",
			expectedArgs: cobra.ExactArgs(1),
		},
		{
			name:         "resume command",
			cmdFunc:      newResumeCmd,
			expectedUse:  "resume <name>",
			expectedArgs: cobra.ExactArgs(1),
		},
		{
			name:         "delete command",
			cmdFunc:      newDeleteCmd,
			expectedUse:  "delete <name>",
			expectedArgs: cobra.ExactArgs(1),
		},
		{
			name:         "clean command",
			cmdFunc:      newCleanCmd,
			expectedUse:  "clean",
			expectedArgs: cobra.NoArgs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()

			assert.Equal(t, tt.expectedUse, cmd.Use)
			assert.NotEmpty(t, cmd.Short)
			assert.NotEmpty(t, cmd.Long)
			assert.NotNil(t, cmd.RunE)

			err := cmd.Args(cmd, make([]string, 0))
			expectedErr := tt.expectedArgs(cmd, make([]string, 0))

			if expectedErr != nil {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestPersistentFlags(t *testing.T) {
	tests := []struct {
		name         string
		flagName     string
		flagType     string
		defaultValue string
	}{
		{
			name:         "base-dir flag",
			flagName:     "base-dir",
			flagType:     "string",
			defaultValue: ".claude/workflow",
		},
		{
			name:         "split-pr flag",
			flagName:     "split-pr",
			flagType:     "bool",
			defaultValue: "false",
		},
		{
			name:         "claude-path flag",
			flagName:     "claude-path",
			flagType:     "string",
			defaultValue: "claude",
		},
		{
			name:         "dangerously-skip-permissions flag",
			flagName:     "dangerously-skip-permissions",
			flagType:     "bool",
			defaultValue: "false",
		},
		{
			name:         "timeout-planning flag",
			flagName:     "timeout-planning",
			flagType:     "duration",
			defaultValue: "1h0m0s",
		},
		{
			name:         "timeout-implementation flag",
			flagName:     "timeout-implementation",
			flagType:     "duration",
			defaultValue: "6h0m0s",
		},
		{
			name:         "timeout-refactoring flag",
			flagName:     "timeout-refactoring",
			flagType:     "duration",
			defaultValue: "6h0m0s",
		},
		{
			name:         "timeout-pr-split flag",
			flagName:     "timeout-pr-split",
			flagType:     "duration",
			defaultValue: "1h0m0s",
		},
		{
			name:         "verbose flag",
			flagName:     "verbose",
			flagType:     "bool",
			defaultValue: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			flag := cmd.PersistentFlags().Lookup(tt.flagName)

			require.NotNil(t, flag, "flag %s should exist", tt.flagName)
			assert.Equal(t, tt.flagType, flag.Value.Type())
			assert.Equal(t, tt.defaultValue, flag.DefValue)
		})
	}
}

func TestVerboseFlag(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantVerbose bool
		wantErr     bool
	}{
		{
			name:        "verbose flag not set defaults to false",
			args:        []string{},
			wantVerbose: false,
			wantErr:     false,
		},
		{
			name:        "verbose flag set with --verbose",
			args:        []string{"--verbose"},
			wantVerbose: true,
			wantErr:     false,
		},
		{
			name:        "verbose flag set with -v",
			args:        []string{"-v"},
			wantVerbose: true,
			wantErr:     false,
		},
		{
			name:        "verbose flag set to true explicitly",
			args:        []string{"--verbose=true"},
			wantVerbose: true,
			wantErr:     false,
		},
		{
			name:        "verbose flag set to false explicitly",
			args:        []string{"--verbose=false"},
			wantVerbose: false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verbose = false
			cmd := newRootCmd()
			cmd.SetArgs(tt.args)

			err := cmd.ParseFlags(tt.args)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantVerbose, verbose)
		})
	}
}

func TestVerboseFlagMetadata(t *testing.T) {
	cmd := newRootCmd()
	flag := cmd.PersistentFlags().Lookup("verbose")

	require.NotNil(t, flag, "verbose flag should exist")
	assert.Equal(t, "bool", flag.Value.Type())
	assert.Equal(t, "false", flag.DefValue)
	assert.Equal(t, "v", flag.Shorthand)
	assert.NotEmpty(t, flag.Usage)
}

func TestCommandArgs(t *testing.T) {
	tests := []struct {
		name       string
		cmdFunc    func() *cobra.Command
		args       []string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "start with correct args",
			cmdFunc:    newStartCmd,
			args:       []string{"test-workflow", "test description"},
			wantErr:    false,
			wantErrMsg: "",
		},
		{
			name:       "start with too few args",
			cmdFunc:    newStartCmd,
			args:       []string{"test-workflow"},
			wantErr:    true,
			wantErrMsg: "accepts 2 arg(s), received 1",
		},
		{
			name:       "start with too many args",
			cmdFunc:    newStartCmd,
			args:       []string{"test-workflow", "description", "extra"},
			wantErr:    true,
			wantErrMsg: "accepts 2 arg(s), received 3",
		},
		{
			name:       "list with no args",
			cmdFunc:    newListCmd,
			args:       []string{},
			wantErr:    false,
			wantErrMsg: "",
		},
		{
			name:       "list with args",
			cmdFunc:    newListCmd,
			args:       []string{"extra"},
			wantErr:    true,
			wantErrMsg: "unknown command",
		},
		{
			name:       "status with correct args",
			cmdFunc:    newStatusCmd,
			args:       []string{"test-workflow"},
			wantErr:    false,
			wantErrMsg: "",
		},
		{
			name:       "status with no args",
			cmdFunc:    newStatusCmd,
			args:       []string{},
			wantErr:    true,
			wantErrMsg: "accepts 1 arg(s), received 0",
		},
		{
			name:       "resume with correct args",
			cmdFunc:    newResumeCmd,
			args:       []string{"test-workflow"},
			wantErr:    false,
			wantErrMsg: "",
		},
		{
			name:       "resume with no args",
			cmdFunc:    newResumeCmd,
			args:       []string{},
			wantErr:    true,
			wantErrMsg: "accepts 1 arg(s), received 0",
		},
		{
			name:       "delete with correct args",
			cmdFunc:    newDeleteCmd,
			args:       []string{"test-workflow"},
			wantErr:    false,
			wantErrMsg: "",
		},
		{
			name:       "delete with no args",
			cmdFunc:    newDeleteCmd,
			args:       []string{},
			wantErr:    true,
			wantErrMsg: "accepts 1 arg(s), received 0",
		},
		{
			name:       "clean with no args",
			cmdFunc:    newCleanCmd,
			args:       []string{},
			wantErr:    false,
			wantErrMsg: "",
		},
		{
			name:       "clean with args",
			cmdFunc:    newCleanCmd,
			args:       []string{"extra"},
			wantErr:    true,
			wantErrMsg: "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()
			err := cmd.Args(cmd, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestHelpText(t *testing.T) {
	tests := []struct {
		name    string
		cmdFunc func() *cobra.Command
	}{
		{
			name:    "root command help",
			cmdFunc: newRootCmd,
		},
		{
			name:    "start command help",
			cmdFunc: newStartCmd,
		},
		{
			name:    "list command help",
			cmdFunc: newListCmd,
		},
		{
			name:    "status command help",
			cmdFunc: newStatusCmd,
		},
		{
			name:    "resume command help",
			cmdFunc: newResumeCmd,
		},
		{
			name:    "delete command help",
			cmdFunc: newDeleteCmd,
		},
		{
			name:    "clean command help",
			cmdFunc: newCleanCmd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)

			err := cmd.Help()
			assert.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestCommandFlags(t *testing.T) {
	tests := []struct {
		name         string
		cmdFunc      func() *cobra.Command
		flagName     string
		flagType     string
		defaultValue string
	}{
		{
			name:         "start command type flag",
			cmdFunc:      newStartCmd,
			flagName:     "type",
			flagType:     "string",
			defaultValue: "",
		},
		{
			name:         "delete command force flag",
			cmdFunc:      newDeleteCmd,
			flagName:     "force",
			flagType:     "bool",
			defaultValue: "false",
		},
		{
			name:         "clean command force flag",
			cmdFunc:      newCleanCmd,
			flagName:     "force",
			flagType:     "bool",
			defaultValue: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()
			flag := cmd.Flags().Lookup(tt.flagName)

			require.NotNil(t, flag)
			assert.Equal(t, tt.flagType, flag.Value.Type())
			assert.Equal(t, tt.defaultValue, flag.DefValue)
		})
	}
}

func TestStartCmd_TypeFlagRequired(t *testing.T) {
	cmd := newStartCmd()
	flag := cmd.Flags().Lookup("type")
	require.NotNil(t, flag)

	annotations := flag.Annotations
	_, required := annotations[cobra.BashCompOneRequiredFlag]
	assert.True(t, required, "type flag should be marked as required")
}

func TestNewStartCmd_Structure(t *testing.T) {
	cmd := newStartCmd()

	assert.Equal(t, "start <name> <description>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE)

	flag := cmd.Flags().Lookup("type")
	require.NotNil(t, flag)
	assert.Equal(t, "string", flag.Value.Type())
}

func TestNewListCmd_Structure(t *testing.T) {
	cmd := newListCmd()

	assert.Equal(t, "list", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Args)
}

func TestNewStatusCmd_Structure(t *testing.T) {
	cmd := newStatusCmd()

	assert.Equal(t, "status <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Args)
}

func TestNewResumeCmd_Structure(t *testing.T) {
	cmd := newResumeCmd()

	assert.Equal(t, "resume <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Args)
}

func TestNewDeleteCmd_Structure(t *testing.T) {
	cmd := newDeleteCmd()

	assert.Equal(t, "delete <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Args)

	flag := cmd.Flags().Lookup("force")
	require.NotNil(t, flag)
	assert.Equal(t, "bool", flag.Value.Type())
	assert.Equal(t, "false", flag.DefValue)
}

func TestNewCleanCmd_Structure(t *testing.T) {
	cmd := newCleanCmd()

	assert.Equal(t, "clean", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Args)

	flag := cmd.Flags().Lookup("force")
	require.NotNil(t, flag)
	assert.Equal(t, "bool", flag.Value.Type())
	assert.Equal(t, "false", flag.DefValue)
}

func TestRootCmd_HasAllSubcommands(t *testing.T) {
	cmd := newRootCmd()

	subcommands := []string{"start", "list", "status", "resume", "delete", "clean"}
	for _, name := range subcommands {
		found := false
		for _, c := range cmd.Commands() {
			if c.Name() == name {
				found = true
				break
			}
		}
		assert.True(t, found, "root command should have %s subcommand", name)
	}
}

func TestCommandValidation(t *testing.T) {
	tests := []struct {
		name        string
		cmdFunc     func() *cobra.Command
		validArgs   []string
		invalidArgs []string
	}{
		{
			name:        "start command validation",
			cmdFunc:     newStartCmd,
			validArgs:   []string{"name", "description"},
			invalidArgs: []string{"only-one"},
		},
		{
			name:        "status command validation",
			cmdFunc:     newStatusCmd,
			validArgs:   []string{"name"},
			invalidArgs: []string{},
		},
		{
			name:        "resume command validation",
			cmdFunc:     newResumeCmd,
			validArgs:   []string{"name"},
			invalidArgs: []string{},
		},
		{
			name:        "delete command validation",
			cmdFunc:     newDeleteCmd,
			validArgs:   []string{"name"},
			invalidArgs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()

			err := cmd.Args(cmd, tt.validArgs)
			assert.NoError(t, err, "valid args should not produce error")

			err = cmd.Args(cmd, tt.invalidArgs)
			assert.Error(t, err, "invalid args should produce error")
		})
	}
}

func TestCreateOrchestrator(t *testing.T) {
	tests := []struct {
		name                       string
		baseDir                    string
		splitPR                    bool
		claudePath                 string
		dangerouslySkipPermissions bool
		timeoutPlanning            time.Duration
		timeoutImplement           time.Duration
		timeoutRefactoring         time.Duration
		timeoutPRSplit             time.Duration
	}{
		{
			name:                       "default values",
			baseDir:                    ".claude/workflow",
			splitPR:                    false,
			claudePath:                 "claude",
			dangerouslySkipPermissions: false,
			timeoutPlanning:            1 * time.Hour,
			timeoutImplement:           6 * time.Hour,
			timeoutRefactoring:         6 * time.Hour,
			timeoutPRSplit:             1 * time.Hour,
		},
		{
			name:                       "custom values",
			baseDir:                    "/tmp/workflows",
			splitPR:                    true,
			claudePath:                 "/usr/local/bin/claude",
			dangerouslySkipPermissions: true,
			timeoutPlanning:            2 * time.Hour,
			timeoutImplement:           8 * time.Hour,
			timeoutRefactoring:         8 * time.Hour,
			timeoutPRSplit:             2 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir = tt.baseDir
			splitPR = tt.splitPR
			claudePath = tt.claudePath
			dangerouslySkipPermissions = tt.dangerouslySkipPermissions
			timeoutPlanning = tt.timeoutPlanning
			timeoutImplement = tt.timeoutImplement
			timeoutRefactoring = tt.timeoutRefactoring
			timeoutPRSplit = tt.timeoutPRSplit

			orchestrator, err := createOrchestrator()

			require.NoError(t, err)
			require.NotNil(t, orchestrator)
		})
	}
}

func TestStartCmd_WorkflowTypeValidation(t *testing.T) {
	tests := []struct {
		name         string
		workflowType string
		wantErr      bool
		wantErrMsg   string
	}{
		{
			name:         "valid feature type",
			workflowType: "feature",
			wantErr:      true,
		},
		{
			name:         "valid fix type",
			workflowType: "fix",
			wantErr:      true,
		},
		{
			name:         "invalid type",
			workflowType: "invalid",
			wantErr:      true,
			wantErrMsg:   "invalid workflow type: invalid (must be 'feature' or 'fix')",
		},
		{
			name:         "empty type",
			workflowType: "",
			wantErr:      true,
			wantErrMsg:   "invalid workflow type:  (must be 'feature' or 'fix')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newStartCmd()

			cmd.SetArgs([]string{"test-workflow", "test description", "--type", tt.workflowType})

			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)

			err := cmd.Execute()

			require.Error(t, err)
			if tt.wantErrMsg != "" {
				assert.Contains(t, err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestStartCmd_MissingTypeFlag(t *testing.T) {
	cmd := newStartCmd()

	cmd.SetArgs([]string{"test-workflow", "test description"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag(s)")
}

func TestDeleteCmd_ForceFlag(t *testing.T) {
	cmd := newDeleteCmd()

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)

	assert.Equal(t, "bool", forceFlag.Value.Type())
	assert.Equal(t, "false", forceFlag.DefValue)

	err := forceFlag.Value.Set("true")
	require.NoError(t, err)
	assert.Equal(t, "true", forceFlag.Value.String())
}

func TestCleanCmd_ForceFlag(t *testing.T) {
	cmd := newCleanCmd()

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)

	assert.Equal(t, "bool", forceFlag.Value.Type())
	assert.Equal(t, "false", forceFlag.DefValue)

	err := forceFlag.Value.Set("true")
	require.NoError(t, err)
	assert.Equal(t, "true", forceFlag.Value.String())
}

func TestPersistentFlagsInheritance(t *testing.T) {
	rootCmd := newRootCmd()

	subcommands := []string{"start", "list", "status", "resume", "delete", "clean"}

	for _, name := range subcommands {
		t.Run(name+" inherits persistent flags", func(t *testing.T) {
			var cmd *cobra.Command
			for _, c := range rootCmd.Commands() {
				if c.Name() == name {
					cmd = c
					break
				}
			}
			require.NotNil(t, cmd, "subcommand %s should exist", name)

			persistentFlags := []string{
				"base-dir",
				"split-pr",
				"claude-path",
				"dangerously-skip-permissions",
				"timeout-planning",
				"timeout-implementation",
				"timeout-refactoring",
				"timeout-pr-split",
			}

			for _, flagName := range persistentFlags {
				flag := cmd.InheritedFlags().Lookup(flagName)
				assert.NotNil(t, flag, "subcommand %s should inherit flag %s", name, flagName)
			}
		})
	}
}

func TestCommandStructureDetails(t *testing.T) {
	tests := []struct {
		name             string
		cmdFunc          func() *cobra.Command
		expectedUse      string
		expectedShort    string
		hasRunE          bool
		hasArgs          bool
		localFlags       []string
		requiredFlags    []string
		expectedArgsFunc func(*cobra.Command, []string) error
	}{
		{
			name:             "start command",
			cmdFunc:          newStartCmd,
			expectedUse:      "start <name> <description>",
			expectedShort:    "Start a new workflow",
			hasRunE:          true,
			hasArgs:          true,
			localFlags:       []string{"type"},
			requiredFlags:    []string{"type"},
			expectedArgsFunc: cobra.ExactArgs(2),
		},
		{
			name:             "list command",
			cmdFunc:          newListCmd,
			expectedUse:      "list",
			expectedShort:    "List all workflows",
			hasRunE:          true,
			hasArgs:          true,
			localFlags:       []string{},
			requiredFlags:    []string{},
			expectedArgsFunc: cobra.NoArgs,
		},
		{
			name:             "status command",
			cmdFunc:          newStatusCmd,
			expectedUse:      "status <name>",
			expectedShort:    "Show workflow status",
			hasRunE:          true,
			hasArgs:          true,
			localFlags:       []string{},
			requiredFlags:    []string{},
			expectedArgsFunc: cobra.ExactArgs(1),
		},
		{
			name:             "resume command",
			cmdFunc:          newResumeCmd,
			expectedUse:      "resume <name>",
			expectedShort:    "Resume an interrupted workflow",
			hasRunE:          true,
			hasArgs:          true,
			localFlags:       []string{},
			requiredFlags:    []string{},
			expectedArgsFunc: cobra.ExactArgs(1),
		},
		{
			name:             "delete command",
			cmdFunc:          newDeleteCmd,
			expectedUse:      "delete <name>",
			expectedShort:    "Delete a workflow",
			hasRunE:          true,
			hasArgs:          true,
			localFlags:       []string{"force"},
			requiredFlags:    []string{},
			expectedArgsFunc: cobra.ExactArgs(1),
		},
		{
			name:             "clean command",
			cmdFunc:          newCleanCmd,
			expectedUse:      "clean",
			expectedShort:    "Delete all completed workflows",
			hasRunE:          true,
			hasArgs:          true,
			localFlags:       []string{"force"},
			requiredFlags:    []string{},
			expectedArgsFunc: cobra.NoArgs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()

			assert.Equal(t, tt.expectedUse, cmd.Use)
			assert.Equal(t, tt.expectedShort, cmd.Short)

			if tt.hasRunE {
				assert.NotNil(t, cmd.RunE, "command should have RunE function")
			}

			if tt.hasArgs {
				assert.NotNil(t, cmd.Args, "command should have Args validation")
			}

			for _, flagName := range tt.localFlags {
				flag := cmd.Flags().Lookup(flagName)
				assert.NotNil(t, flag, "command should have flag %s", flagName)
			}

			for _, flagName := range tt.requiredFlags {
				flag := cmd.Flags().Lookup(flagName)
				require.NotNil(t, flag, "required flag %s should exist", flagName)
				annotations := flag.Annotations
				_, required := annotations[cobra.BashCompOneRequiredFlag]
				assert.True(t, required, "flag %s should be marked as required", flagName)
			}
		})
	}
}

func TestRootCmd_UsageText(t *testing.T) {
	cmd := newRootCmd()

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Help()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "claude-workflow")
	assert.Contains(t, output, "Available Commands:")
	assert.Contains(t, output, "start")
	assert.Contains(t, output, "list")
	assert.Contains(t, output, "status")
	assert.Contains(t, output, "resume")
	assert.Contains(t, output, "delete")
	assert.Contains(t, output, "clean")
}

func TestCommandLongDescriptions(t *testing.T) {
	tests := []struct {
		name     string
		cmdFunc  func() *cobra.Command
		contains string
	}{
		{
			name:     "root command",
			cmdFunc:  newRootCmd,
			contains: "multi-phase development workflows",
		},
		{
			name:     "start command",
			cmdFunc:  newStartCmd,
			contains: "Start a new workflow",
		},
		{
			name:     "list command",
			cmdFunc:  newListCmd,
			contains: "List all workflows",
		},
		{
			name:     "status command",
			cmdFunc:  newStatusCmd,
			contains: "detailed status",
		},
		{
			name:     "resume command",
			cmdFunc:  newResumeCmd,
			contains: "Resume a workflow",
		},
		{
			name:     "delete command",
			cmdFunc:  newDeleteCmd,
			contains: "Delete a workflow",
		},
		{
			name:     "clean command",
			cmdFunc:  newCleanCmd,
			contains: "completed successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()
			assert.NotEmpty(t, cmd.Long)
			assert.Contains(t, strings.ToLower(cmd.Long), strings.ToLower(tt.contains))
		})
	}
}

func TestPersistentFlagDefaults(t *testing.T) {
	cmd := newRootCmd()

	baseDir := cmd.PersistentFlags().Lookup("base-dir")
	assert.Equal(t, ".claude/workflow", baseDir.DefValue)

	splitPR := cmd.PersistentFlags().Lookup("split-pr")
	assert.Equal(t, "false", splitPR.DefValue)

	claudePath := cmd.PersistentFlags().Lookup("claude-path")
	assert.Equal(t, "claude", claudePath.DefValue)

	dangerouslySkipPermissions := cmd.PersistentFlags().Lookup("dangerously-skip-permissions")
	assert.Equal(t, "false", dangerouslySkipPermissions.DefValue)

	timeoutPlanning := cmd.PersistentFlags().Lookup("timeout-planning")
	assert.Equal(t, "1h0m0s", timeoutPlanning.DefValue)

	timeoutImplementation := cmd.PersistentFlags().Lookup("timeout-implementation")
	assert.Equal(t, "6h0m0s", timeoutImplementation.DefValue)

	timeoutRefactoring := cmd.PersistentFlags().Lookup("timeout-refactoring")
	assert.Equal(t, "6h0m0s", timeoutRefactoring.DefValue)

	timeoutPRSplit := cmd.PersistentFlags().Lookup("timeout-pr-split")
	assert.Equal(t, "1h0m0s", timeoutPRSplit.DefValue)
}

func TestNewListCmd_DetailedStructure(t *testing.T) {
	cmd := newListCmd()

	assert.Equal(t, "list", cmd.Use)
	assert.Equal(t, "List all workflows", cmd.Short)
	assert.Contains(t, cmd.Long, "List all workflows")
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Args)

	err := cmd.Args(cmd, []string{})
	assert.NoError(t, err)

	err = cmd.Args(cmd, []string{"extra"})
	assert.Error(t, err)
}

func TestNewStatusCmd_Details(t *testing.T) {
	cmd := newStatusCmd()

	assert.Equal(t, "status <name>", cmd.Use)
	assert.Equal(t, "Show workflow status", cmd.Short)
	assert.Contains(t, cmd.Long, "detailed status")
	assert.NotNil(t, cmd.RunE)

	err := cmd.Args(cmd, []string{"workflow-name"})
	assert.NoError(t, err)

	err = cmd.Args(cmd, []string{})
	assert.Error(t, err)

	err = cmd.Args(cmd, []string{"name1", "name2"})
	assert.Error(t, err)
}

func TestNewResumeCmd_Details(t *testing.T) {
	cmd := newResumeCmd()

	assert.Equal(t, "resume <name>", cmd.Use)
	assert.Equal(t, "Resume an interrupted workflow", cmd.Short)
	assert.Contains(t, cmd.Long, "Resume a workflow")
	assert.NotNil(t, cmd.RunE)

	err := cmd.Args(cmd, []string{"workflow-name"})
	assert.NoError(t, err)

	err = cmd.Args(cmd, []string{})
	assert.Error(t, err)

	err = cmd.Args(cmd, []string{"name1", "name2"})
	assert.Error(t, err)
}

func TestNewDeleteCmd_Details(t *testing.T) {
	cmd := newDeleteCmd()

	assert.Equal(t, "delete <name>", cmd.Use)
	assert.Equal(t, "Delete a workflow", cmd.Short)
	assert.Contains(t, cmd.Long, "Delete a workflow")
	assert.NotNil(t, cmd.RunE)

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "bool", forceFlag.Value.Type())

	err := cmd.Args(cmd, []string{"workflow-name"})
	assert.NoError(t, err)

	err = cmd.Args(cmd, []string{})
	assert.Error(t, err)

	err = cmd.Args(cmd, []string{"name1", "name2"})
	assert.Error(t, err)
}

func TestNewCleanCmd_Details(t *testing.T) {
	cmd := newCleanCmd()

	assert.Equal(t, "clean", cmd.Use)
	assert.Equal(t, "Delete all completed workflows", cmd.Short)
	assert.Contains(t, cmd.Long, "completed successfully")
	assert.NotNil(t, cmd.RunE)

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "bool", forceFlag.Value.Type())

	err := cmd.Args(cmd, []string{})
	assert.NoError(t, err)

	err = cmd.Args(cmd, []string{"extra"})
	assert.Error(t, err)
}

func TestAllCommands_HaveRunE(t *testing.T) {
	commands := []struct {
		name    string
		cmdFunc func() *cobra.Command
	}{
		{"start", newStartCmd},
		{"list", newListCmd},
		{"status", newStatusCmd},
		{"resume", newResumeCmd},
		{"delete", newDeleteCmd},
		{"clean", newCleanCmd},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmdFunc()
			assert.NotNil(t, cmd.RunE, "%s command should have RunE", tc.name)
		})
	}
}

func TestCommandUsageAndShort(t *testing.T) {
	tests := []struct {
		name          string
		cmdFunc       func() *cobra.Command
		expectedUse   string
		expectedShort string
	}{
		{
			name:          "root",
			cmdFunc:       newRootCmd,
			expectedUse:   "claude-workflow",
			expectedShort: "Orchestrate multi-phase development workflows with Claude Code CLI",
		},
		{
			name:          "start",
			cmdFunc:       newStartCmd,
			expectedUse:   "start <name> <description>",
			expectedShort: "Start a new workflow",
		},
		{
			name:          "list",
			cmdFunc:       newListCmd,
			expectedUse:   "list",
			expectedShort: "List all workflows",
		},
		{
			name:          "status",
			cmdFunc:       newStatusCmd,
			expectedUse:   "status <name>",
			expectedShort: "Show workflow status",
		},
		{
			name:          "resume",
			cmdFunc:       newResumeCmd,
			expectedUse:   "resume <name>",
			expectedShort: "Resume an interrupted workflow",
		},
		{
			name:          "delete",
			cmdFunc:       newDeleteCmd,
			expectedUse:   "delete <name>",
			expectedShort: "Delete a workflow",
		},
		{
			name:          "clean",
			cmdFunc:       newCleanCmd,
			expectedUse:   "clean",
			expectedShort: "Delete all completed workflows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()
			assert.Equal(t, tt.expectedUse, cmd.Use)
			assert.Equal(t, tt.expectedShort, cmd.Short)
		})
	}
}

func TestFlagUsage(t *testing.T) {
	tests := []struct {
		name     string
		cmdFunc  func() *cobra.Command
		flagName string
		usage    string
	}{
		{
			name:     "start type flag",
			cmdFunc:  newStartCmd,
			flagName: "type",
			usage:    "workflow type (feature or fix)",
		},
		{
			name:     "delete force flag",
			cmdFunc:  newDeleteCmd,
			flagName: "force",
			usage:    "skip confirmation prompt",
		},
		{
			name:     "clean force flag",
			cmdFunc:  newCleanCmd,
			flagName: "force",
			usage:    "skip confirmation prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()
			flag := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, flag)
			assert.Equal(t, tt.usage, flag.Usage)
		})
	}
}

func TestPersistentFlagUsage(t *testing.T) {
	cmd := newRootCmd()

	tests := []struct {
		flagName string
		usage    string
	}{
		{
			flagName: "base-dir",
			usage:    "base directory for workflows",
		},
		{
			flagName: "split-pr",
			usage:    "enable PR split phase to split large PRs into smaller child PRs",
		},
		{
			flagName: "claude-path",
			usage:    "path to claude CLI",
		},
		{
			flagName: "dangerously-skip-permissions",
			usage:    "skip all permission prompts in Claude Code (use with caution)",
		},
		{
			flagName: "timeout-planning",
			usage:    "planning phase timeout",
		},
		{
			flagName: "timeout-implementation",
			usage:    "implementation phase timeout",
		},
		{
			flagName: "timeout-refactoring",
			usage:    "refactoring phase timeout",
		},
		{
			flagName: "timeout-pr-split",
			usage:    "PR split phase timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := cmd.PersistentFlags().Lookup(tt.flagName)
			require.NotNil(t, flag)
			assert.Equal(t, tt.usage, flag.Usage)
		})
	}
}

func TestStartCmd_InvalidWorkflowType(t *testing.T) {
	tests := []struct {
		name         string
		workflowType string
		wantErrMsg   string
	}{
		{
			name:         "invalid workflow type",
			workflowType: "bug",
			wantErrMsg:   "invalid workflow type: bug (must be 'feature' or 'fix')",
		},
		{
			name:         "empty workflow type results in error",
			workflowType: "",
			wantErrMsg:   "invalid workflow type:  (must be 'feature' or 'fix')",
		},
		{
			name:         "numeric workflow type",
			workflowType: "123",
			wantErrMsg:   "invalid workflow type: 123 (must be 'feature' or 'fix')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := newRootCmd()
			rootCmd.SetArgs([]string{"start", "test-name", "test-desc", "--type", tt.workflowType})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestCommands_ArgsValidation(t *testing.T) {
	tests := []struct {
		name    string
		cmdArgs []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "start with insufficient args",
			cmdArgs: []string{"start", "name"},
			wantErr: true,
			errMsg:  "accepts 2 arg(s), received 1",
		},
		{
			name:    "start with too many args",
			cmdArgs: []string{"start", "name", "desc", "extra", "--type", "feature"},
			wantErr: true,
			errMsg:  "accepts 2 arg(s), received 3",
		},
		{
			name:    "status without args",
			cmdArgs: []string{"status"},
			wantErr: true,
			errMsg:  "accepts 1 arg(s), received 0",
		},
		{
			name:    "status with too many args",
			cmdArgs: []string{"status", "name1", "name2"},
			wantErr: true,
			errMsg:  "accepts 1 arg(s), received 2",
		},
		{
			name:    "resume without args",
			cmdArgs: []string{"resume"},
			wantErr: true,
			errMsg:  "accepts 1 arg(s), received 0",
		},
		{
			name:    "resume with too many args",
			cmdArgs: []string{"resume", "name1", "name2"},
			wantErr: true,
			errMsg:  "accepts 1 arg(s), received 2",
		},
		{
			name:    "delete without args",
			cmdArgs: []string{"delete"},
			wantErr: true,
			errMsg:  "accepts 1 arg(s), received 0",
		},
		{
			name:    "delete with too many args",
			cmdArgs: []string{"delete", "name1", "name2"},
			wantErr: true,
			errMsg:  "accepts 1 arg(s), received 2",
		},
		{
			name:    "list with args",
			cmdArgs: []string{"list", "extra"},
			wantErr: true,
			errMsg:  "unknown command",
		},
		{
			name:    "clean with args",
			cmdArgs: []string{"clean", "extra"},
			wantErr: true,
			errMsg:  "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := newRootCmd()
			rootCmd.SetArgs(tt.cmdArgs)

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRootCmd_SubcommandExistence(t *testing.T) {
	rootCmd := newRootCmd()

	tests := []struct {
		cmdName string
	}{
		{cmdName: "start"},
		{cmdName: "list"},
		{cmdName: "status"},
		{cmdName: "resume"},
		{cmdName: "delete"},
		{cmdName: "clean"},
	}

	for _, tt := range tests {
		t.Run(tt.cmdName, func(t *testing.T) {
			cmd, _, err := rootCmd.Find([]string{tt.cmdName})
			require.NoError(t, err)
			assert.Equal(t, tt.cmdName, cmd.Name())
		})
	}
}

func TestRootCmd_InvalidSubcommand(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"invalid-command"})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestCommandCreation_AllFunctionsReturn(t *testing.T) {
	tests := []struct {
		name    string
		cmdFunc func() *cobra.Command
	}{
		{"newRootCmd", newRootCmd},
		{"newStartCmd", newStartCmd},
		{"newListCmd", newListCmd},
		{"newStatusCmd", newStatusCmd},
		{"newResumeCmd", newResumeCmd},
		{"newDeleteCmd", newDeleteCmd},
		{"newCleanCmd", newCleanCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()
			assert.NotNil(t, cmd)
			assert.NotEmpty(t, cmd.Use)
			assert.NotEmpty(t, cmd.Short)
			assert.NotEmpty(t, cmd.Long)
		})
	}
}

func TestListCmd_ExecutionWithoutWorkflows(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"list", "--base-dir", "/tmp/nonexistent-workflow-dir-test"})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.NoError(t, err)
}

func TestStatusCmd_ExecutionWithNonexistentWorkflow(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"status", "nonexistent-workflow", "--base-dir", "/tmp/nonexistent-workflow-dir-test"})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.Error(t, err)
}

func TestResumeCmd_ExecutionWithNonexistentWorkflow(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"resume", "nonexistent-workflow", "--base-dir", "/tmp/nonexistent-workflow-dir-test"})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.Error(t, err)
}

func TestDeleteCmd_ExecutionWithNonexistentWorkflow(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"delete", "nonexistent-workflow", "--force", "--base-dir", "/tmp/nonexistent-workflow-dir-test"})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.Error(t, err)
}

func TestCleanCmd_ExecutionWithoutWorkflows(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"clean", "--force", "--base-dir", "/tmp/nonexistent-workflow-dir-test"})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.NoError(t, err)
}

func TestNewStartCmd_FlagType(t *testing.T) {
	cmd := newStartCmd()

	typeFlag := cmd.Flags().Lookup("type")
	require.NotNil(t, typeFlag)

	err := typeFlag.Value.Set("feature")
	require.NoError(t, err)
	assert.Equal(t, "feature", typeFlag.Value.String())

	err = typeFlag.Value.Set("fix")
	require.NoError(t, err)
	assert.Equal(t, "fix", typeFlag.Value.String())
}

func TestNewDeleteCmd_FlagModification(t *testing.T) {
	cmd := newDeleteCmd()

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)

	assert.Equal(t, "false", forceFlag.DefValue)

	err := forceFlag.Value.Set("true")
	require.NoError(t, err)
	assert.Equal(t, "true", forceFlag.Value.String())

	err = forceFlag.Value.Set("false")
	require.NoError(t, err)
	assert.Equal(t, "false", forceFlag.Value.String())
}

func TestNewCleanCmd_FlagModification(t *testing.T) {
	cmd := newCleanCmd()

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)

	assert.Equal(t, "false", forceFlag.DefValue)

	err := forceFlag.Value.Set("true")
	require.NoError(t, err)
	assert.Equal(t, "true", forceFlag.Value.String())
}

func TestPersistentFlags_Modification(t *testing.T) {
	cmd := newRootCmd()

	tests := []struct {
		flagName string
		setValue string
	}{
		{"base-dir", "/custom/path"},
		{"split-pr", "true"},
		{"claude-path", "/custom/claude"},
		{"dangerously-skip-permissions", "true"},
		{"timeout-planning", "2h"},
		{"timeout-implementation", "8h"},
		{"timeout-refactoring", "8h"},
		{"timeout-pr-split", "2h"},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := cmd.PersistentFlags().Lookup(tt.flagName)
			require.NotNil(t, flag)

			err := flag.Value.Set(tt.setValue)
			assert.NoError(t, err)
		})
	}
}

func TestCommandChaining(t *testing.T) {
	rootCmd := newRootCmd()

	commandNames := []string{"start", "list", "status", "resume", "delete", "clean"}

	for _, name := range commandNames {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == name {
				found = true
				assert.NotNil(t, cmd.Use)
				assert.NotNil(t, cmd.Short)
				assert.NotNil(t, cmd.Long)
				break
			}
		}
		assert.True(t, found, "Command %s should be registered", name)
	}
}

func TestStartCmd_ValidWorkflowTypes(t *testing.T) {
	tests := []struct {
		name         string
		workflowType string
		wantErr      bool
	}{
		{
			name:         "feature type",
			workflowType: "feature",
			wantErr:      true,
		},
		{
			name:         "fix type",
			workflowType: "fix",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := newRootCmd()
			rootCmd.SetArgs([]string{"start", "test-workflow", "test description", "--type", tt.workflowType})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			if tt.wantErr {
				assert.Error(t, err)
			}
		})
	}
}

func TestAllCommands_MinimalExecution(t *testing.T) {
	tests := []struct {
		name        string
		cmdArgs     []string
		expectError bool
	}{
		{
			name:        "list command execution",
			cmdArgs:     []string{"list", "--base-dir", "/tmp/test-workflows-minimal"},
			expectError: false,
		},
		{
			name:        "status command with name",
			cmdArgs:     []string{"status", "test", "--base-dir", "/tmp/test-workflows-minimal"},
			expectError: true,
		},
		{
			name:        "resume command with name",
			cmdArgs:     []string{"resume", "test", "--base-dir", "/tmp/test-workflows-minimal"},
			expectError: true,
		},
		{
			name:        "delete command with force",
			cmdArgs:     []string{"delete", "test", "--force", "--base-dir", "/tmp/test-workflows-minimal"},
			expectError: true,
		},
		{
			name:        "clean command with force",
			cmdArgs:     []string{"clean", "--force", "--base-dir", "/tmp/test-workflows-minimal"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := newRootCmd()
			rootCmd.SetArgs(tt.cmdArgs)

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestListCmd_WithWorkflows(t *testing.T) {
	tempDir := t.TempDir()

	state := workflow.WorkflowState{
		Version:      "1.0",
		Name:         "test-workflow",
		Type:         workflow.WorkflowTypeFeature,
		Description:  "test description",
		CurrentPhase: workflow.PhaseCompleted,
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		UpdatedAt:    time.Now(),
		Phases:       make(map[workflow.Phase]*workflow.PhaseState),
	}

	workflowDir := filepath.Join(tempDir, "test-workflow")
	err := os.MkdirAll(workflowDir, 0755)
	require.NoError(t, err)

	stateFile := filepath.Join(workflowDir, "state.json")
	data, err := json.Marshal(state)
	require.NoError(t, err)

	err = os.WriteFile(stateFile, data, 0644)
	require.NoError(t, err)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"list", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err = rootCmd.Execute()

	assert.NoError(t, err)
}

func TestCleanCmd_WithCompletedWorkflows(t *testing.T) {
	tempDir := t.TempDir()

	state := workflow.WorkflowState{
		Version:      "1.0",
		Name:         "completed-workflow",
		Type:         workflow.WorkflowTypeFeature,
		Description:  "completed test",
		CurrentPhase: workflow.PhaseCompleted,
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		UpdatedAt:    time.Now(),
		Phases:       make(map[workflow.Phase]*workflow.PhaseState),
	}

	workflowDir := filepath.Join(tempDir, "completed-workflow")
	err := os.MkdirAll(workflowDir, 0755)
	require.NoError(t, err)

	stateFile := filepath.Join(workflowDir, "state.json")
	data, err := json.Marshal(state)
	require.NoError(t, err)

	err = os.WriteFile(stateFile, data, 0644)
	require.NoError(t, err)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"clean", "--force", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err = rootCmd.Execute()

	assert.NoError(t, err)
}

func TestDeleteCmd_WithExistingWorkflow(t *testing.T) {
	tempDir := t.TempDir()

	state := workflow.WorkflowState{
		Version:      "1.0",
		Name:         "delete-test",
		Type:         workflow.WorkflowTypeFix,
		Description:  "test delete",
		CurrentPhase: workflow.PhasePlanning,
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		UpdatedAt:    time.Now(),
		Phases:       make(map[workflow.Phase]*workflow.PhaseState),
	}

	workflowDir := filepath.Join(tempDir, "delete-test")
	err := os.MkdirAll(workflowDir, 0755)
	require.NoError(t, err)

	stateFile := filepath.Join(workflowDir, "state.json")
	data, err := json.Marshal(state)
	require.NoError(t, err)

	err = os.WriteFile(stateFile, data, 0644)
	require.NoError(t, err)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"delete", "delete-test", "--force", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err = rootCmd.Execute()

	assert.NoError(t, err)
}

func TestStatusCmd_WithExistingWorkflow(t *testing.T) {
	tempDir := t.TempDir()

	state := workflow.WorkflowState{
		Version:      "1.0",
		Name:         "status-test",
		Type:         workflow.WorkflowTypeFeature,
		Description:  "test status",
		CurrentPhase: workflow.PhasePlanning,
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		UpdatedAt:    time.Now(),
		Phases:       make(map[workflow.Phase]*workflow.PhaseState),
	}

	workflowDir := filepath.Join(tempDir, "status-test")
	err := os.MkdirAll(workflowDir, 0755)
	require.NoError(t, err)

	stateFile := filepath.Join(workflowDir, "state.json")
	data, err := json.Marshal(state)
	require.NoError(t, err)

	err = os.WriteFile(stateFile, data, 0644)
	require.NoError(t, err)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"status", "status-test", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err = rootCmd.Execute()

	assert.NoError(t, err)
}

func TestListCmd_OrchestratorCreationError(t *testing.T) {
	tests := []struct {
		name    string
		baseDir string
		wantErr bool
	}{
		{
			name:    "empty base dir causes error",
			baseDir: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := newRootCmd()
			rootCmd.SetArgs([]string{"list", "--base-dir", tt.baseDir})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeleteCmd_OrchestratorCreationError(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"delete", "test-workflow", "--force", "--base-dir", ""})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.Error(t, err)
}

func TestCleanCmd_OrchestratorCreationError(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"clean", "--force", "--base-dir", ""})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.Error(t, err)
}

func TestResumeCmd_OrchestratorCreationError(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"resume", "test-workflow", "--base-dir", ""})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.Error(t, err)
}

func TestListCmd_WithMultipleWorkflows(t *testing.T) {
	tempDir := t.TempDir()

	workflows := []struct {
		name   string
		status workflow.Phase
	}{
		{"workflow-1", workflow.PhasePlanning},
		{"workflow-2", workflow.PhaseCompleted},
		{"workflow-3", workflow.PhaseFailed},
	}

	for _, wf := range workflows {
		state := workflow.WorkflowState{
			Version:      "1.0",
			Name:         wf.name,
			Type:         workflow.WorkflowTypeFeature,
			Description:  "test workflow",
			CurrentPhase: wf.status,
			CreatedAt:    time.Now().Add(-1 * time.Hour),
			UpdatedAt:    time.Now(),
			Phases:       make(map[workflow.Phase]*workflow.PhaseState),
		}

		workflowDir := filepath.Join(tempDir, wf.name)
		err := os.MkdirAll(workflowDir, 0755)
		require.NoError(t, err)

		stateFile := filepath.Join(workflowDir, "state.json")
		data, err := json.Marshal(state)
		require.NoError(t, err)

		err = os.WriteFile(stateFile, data, 0644)
		require.NoError(t, err)
	}

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"list", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.NoError(t, err)
}

func TestCleanCmd_WithMixedWorkflows(t *testing.T) {
	tempDir := t.TempDir()

	workflows := []struct {
		name   string
		status workflow.Phase
	}{
		{"completed-1", workflow.PhaseCompleted},
		{"in-progress", workflow.PhasePlanning},
		{"completed-2", workflow.PhaseCompleted},
		{"failed", workflow.PhaseFailed},
	}

	for _, wf := range workflows {
		state := workflow.WorkflowState{
			Version:      "1.0",
			Name:         wf.name,
			Type:         workflow.WorkflowTypeFeature,
			Description:  "test workflow",
			CurrentPhase: wf.status,
			CreatedAt:    time.Now().Add(-1 * time.Hour),
			UpdatedAt:    time.Now(),
			Phases:       make(map[workflow.Phase]*workflow.PhaseState),
		}

		workflowDir := filepath.Join(tempDir, wf.name)
		err := os.MkdirAll(workflowDir, 0755)
		require.NoError(t, err)

		stateFile := filepath.Join(workflowDir, "state.json")
		data, err := json.Marshal(state)
		require.NoError(t, err)

		err = os.WriteFile(stateFile, data, 0644)
		require.NoError(t, err)
	}

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"clean", "--force", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.NoError(t, err)

	remainingFiles, _ := os.ReadDir(tempDir)
	completedCount := 0
	for _, f := range remainingFiles {
		if f.Name() == "completed-1" || f.Name() == "completed-2" {
			completedCount++
		}
	}
	assert.Equal(t, 0, completedCount, "completed workflows should be deleted")
}

func TestDeleteCmd_ConfirmationPromptCancelled(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "user responds no",
			input: "no\n",
		},
		{
			name:  "user responds n",
			input: "n\n",
		},
		{
			name:  "user responds with empty line",
			input: "\n",
		},
		{
			name:  "user responds with random text",
			input: "maybe\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			state := workflow.WorkflowState{
				Version:      "1.0",
				Name:         "test-workflow",
				Type:         workflow.WorkflowTypeFeature,
				Description:  "test",
				CurrentPhase: workflow.PhasePlanning,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				Phases:       make(map[workflow.Phase]*workflow.PhaseState),
			}

			workflowDir := filepath.Join(tempDir, "test-workflow")
			err := os.MkdirAll(workflowDir, 0755)
			require.NoError(t, err)

			stateFile := filepath.Join(workflowDir, "state.json")
			data, err := json.Marshal(state)
			require.NoError(t, err)

			err = os.WriteFile(stateFile, data, 0644)
			require.NoError(t, err)

			oldStdin := os.Stdin
			defer func() { os.Stdin = oldStdin }()

			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdin = r

			_, err = w.Write([]byte(tt.input))
			require.NoError(t, err)
			w.Close()

			rootCmd := newRootCmd()
			rootCmd.SetArgs([]string{"delete", "test-workflow", "--base-dir", tempDir})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err = rootCmd.Execute()

			assert.NoError(t, err)

			_, err = os.Stat(workflowDir)
			assert.NoError(t, err, "workflow should still exist after cancellation")
		})
	}
}

func TestDeleteCmd_ConfirmationPromptAccepted(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "user responds yes",
			input: "yes\n",
		},
		{
			name:  "user responds y",
			input: "y\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			state := workflow.WorkflowState{
				Version:      "1.0",
				Name:         "test-workflow",
				Type:         workflow.WorkflowTypeFeature,
				Description:  "test",
				CurrentPhase: workflow.PhasePlanning,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				Phases:       make(map[workflow.Phase]*workflow.PhaseState),
			}

			workflowDir := filepath.Join(tempDir, "test-workflow")
			err := os.MkdirAll(workflowDir, 0755)
			require.NoError(t, err)

			stateFile := filepath.Join(workflowDir, "state.json")
			data, err := json.Marshal(state)
			require.NoError(t, err)

			err = os.WriteFile(stateFile, data, 0644)
			require.NoError(t, err)

			oldStdin := os.Stdin
			defer func() { os.Stdin = oldStdin }()

			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdin = r

			_, err = w.Write([]byte(tt.input))
			require.NoError(t, err)
			w.Close()

			rootCmd := newRootCmd()
			rootCmd.SetArgs([]string{"delete", "test-workflow", "--base-dir", tempDir})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err = rootCmd.Execute()

			assert.NoError(t, err)

			_, err = os.Stat(workflowDir)
			assert.True(t, os.IsNotExist(err), "workflow should be deleted")
		})
	}
}

func TestDeleteCmd_OrchestratorDeleteError(t *testing.T) {
	tempDir := t.TempDir()

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"delete", "nonexistent-workflow", "--force", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete workflow")
}

func TestCleanCmd_NoCompletedWorkflows(t *testing.T) {
	tempDir := t.TempDir()

	workflows := []struct {
		name   string
		status workflow.Phase
	}{
		{"in-progress-1", workflow.PhasePlanning},
		{"failed-1", workflow.PhaseFailed},
	}

	for _, wf := range workflows {
		state := workflow.WorkflowState{
			Version:      "1.0",
			Name:         wf.name,
			Type:         workflow.WorkflowTypeFeature,
			Description:  "test workflow",
			CurrentPhase: wf.status,
			CreatedAt:    time.Now().Add(-1 * time.Hour),
			UpdatedAt:    time.Now(),
			Phases:       make(map[workflow.Phase]*workflow.PhaseState),
		}

		workflowDir := filepath.Join(tempDir, wf.name)
		err := os.MkdirAll(workflowDir, 0755)
		require.NoError(t, err)

		stateFile := filepath.Join(workflowDir, "state.json")
		data, err := json.Marshal(state)
		require.NoError(t, err)

		err = os.WriteFile(stateFile, data, 0644)
		require.NoError(t, err)
	}

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"clean", "--force", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	assert.NoError(t, err)

	for _, wf := range workflows {
		workflowDir := filepath.Join(tempDir, wf.name)
		_, err := os.Stat(workflowDir)
		assert.NoError(t, err, "non-completed workflow %s should not be deleted", wf.name)
	}
}

func TestCleanCmd_CleanError(t *testing.T) {
	tempDir := t.TempDir()

	err := os.Chmod(tempDir, 0000)
	require.NoError(t, err)

	defer func() {
		os.Chmod(tempDir, 0755)
	}()

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"clean", "--force", "--base-dir", tempDir})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err = rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to")
}

func TestCleanCmd_ConfirmationPromptCancelled(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "user responds no",
			input: "no\n",
		},
		{
			name:  "user responds n",
			input: "n\n",
		},
		{
			name:  "user responds with empty line",
			input: "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			state := workflow.WorkflowState{
				Version:      "1.0",
				Name:         "completed-workflow",
				Type:         workflow.WorkflowTypeFeature,
				Description:  "completed test",
				CurrentPhase: workflow.PhaseCompleted,
				CreatedAt:    time.Now().Add(-1 * time.Hour),
				UpdatedAt:    time.Now(),
				Phases:       make(map[workflow.Phase]*workflow.PhaseState),
			}

			workflowDir := filepath.Join(tempDir, "completed-workflow")
			err := os.MkdirAll(workflowDir, 0755)
			require.NoError(t, err)

			stateFile := filepath.Join(workflowDir, "state.json")
			data, err := json.Marshal(state)
			require.NoError(t, err)

			err = os.WriteFile(stateFile, data, 0644)
			require.NoError(t, err)

			oldStdin := os.Stdin
			defer func() { os.Stdin = oldStdin }()

			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdin = r

			_, err = w.Write([]byte(tt.input))
			require.NoError(t, err)
			w.Close()

			rootCmd := newRootCmd()
			rootCmd.SetArgs([]string{"clean", "--base-dir", tempDir})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err = rootCmd.Execute()

			assert.NoError(t, err)

			_, err = os.Stat(workflowDir)
			assert.NoError(t, err, "workflow should still exist after cancellation")
		})
	}
}

func TestStartCmd_UpdatePRFlag(t *testing.T) {
	cmd := newStartCmd()

	updatePRFlag := cmd.Flags().Lookup("update-pr")
	require.NotNil(t, updatePRFlag)
	assert.Equal(t, "int", updatePRFlag.Value.Type())
	assert.Equal(t, "0", updatePRFlag.DefValue)
	assert.Equal(t, "update an existing PR instead of creating a new one (PR number)", updatePRFlag.Usage)
}

func TestStartCmd_UpdatePRAndSplitPRMutualExclusivity(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "both update-pr and split-pr set should fail",
			args:    []string{"start", "test-workflow", "test description", "--type", "feature", "--update-pr", "123", "--split-pr"},
			wantErr: true,
			errMsg:  "cannot use --update-pr and --split-pr together",
		},
		{
			name:    "only update-pr set should succeed with orchestrator call",
			args:    []string{"start", "test-workflow", "test description", "--type", "feature", "--update-pr", "123"},
			wantErr: true,
		},
		{
			name:    "only split-pr set should succeed with orchestrator call",
			args:    []string{"start", "test-workflow", "test description", "--type", "feature", "--split-pr"},
			wantErr: true,
		},
		{
			name:    "neither flag set should succeed with orchestrator call",
			args:    []string{"start", "test-workflow", "test description", "--type", "feature"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := newRootCmd()
			rootCmd.SetArgs(tt.args)

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStartCmd_UpdatePRFlagValue(t *testing.T) {
	tests := []struct {
		name         string
		updatePRFlag string
		wantValue    string
	}{
		{
			name:         "default value is 0",
			updatePRFlag: "",
			wantValue:    "0",
		},
		{
			name:         "valid PR number",
			updatePRFlag: "123",
			wantValue:    "123",
		},
		{
			name:         "another valid PR number",
			updatePRFlag: "999",
			wantValue:    "999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newStartCmd()
			updatePRFlag := cmd.Flags().Lookup("update-pr")
			require.NotNil(t, updatePRFlag)

			if tt.updatePRFlag != "" {
				err := updatePRFlag.Value.Set(tt.updatePRFlag)
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantValue, updatePRFlag.Value.String())
		})
	}
}

func TestStartCmd_HelpTextWithExamples(t *testing.T) {
	cmd := newStartCmd()

	assert.Contains(t, cmd.Long, "Examples:")
	assert.Contains(t, cmd.Long, "workflow start my-feature \"Add new feature\"")
	assert.Contains(t, cmd.Long, "workflow start my-feature \"Add new feature\" --update-pr 123")
}

func TestStartCmd_SkipToFlag(t *testing.T) {
	cmd := newStartCmd()

	skipToFlag := cmd.Flags().Lookup("skip-to")
	require.NotNil(t, skipToFlag)
	assert.Equal(t, "string", skipToFlag.Value.Type())
	assert.Equal(t, "", skipToFlag.DefValue)
}

func TestStartCmd_WithPlanFlag(t *testing.T) {
	cmd := newStartCmd()

	withPlanFlag := cmd.Flags().Lookup("with-plan")
	require.NotNil(t, withPlanFlag)
	assert.Equal(t, "string", withPlanFlag.Value.Type())
	assert.Equal(t, "", withPlanFlag.DefValue)
}

func TestResumeCmd_SkipToFlag(t *testing.T) {
	cmd := newResumeCmd()

	skipToFlag := cmd.Flags().Lookup("skip-to")
	require.NotNil(t, skipToFlag)
	assert.Equal(t, "string", skipToFlag.Value.Type())
	assert.Equal(t, "", skipToFlag.DefValue)
}

func TestRootCmd_ForceBackwardFlag(t *testing.T) {
	cmd := newRootCmd()

	forceBackwardFlag := cmd.PersistentFlags().Lookup("force-backward")
	require.NotNil(t, forceBackwardFlag)
	assert.Equal(t, "bool", forceBackwardFlag.Value.Type())
	assert.Equal(t, "false", forceBackwardFlag.DefValue)
}

func TestStartCmd_SkipToValidation(t *testing.T) {
	tests := []struct {
		name       string
		skipTo     string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "valid planning phase",
			skipTo:     "planning",
			wantErr:    true,
			wantErrMsg: "",
		},
		{
			name:       "valid confirmation phase",
			skipTo:     "confirmation",
			wantErr:    true,
			wantErrMsg: "",
		},
		{
			name:       "valid implementation phase",
			skipTo:     "implementation",
			wantErr:    true,
			wantErrMsg: "",
		},
		{
			name:       "valid refactoring phase",
			skipTo:     "refactoring",
			wantErr:    true,
			wantErrMsg: "",
		},
		{
			name:       "valid pr-split phase",
			skipTo:     "pr-split",
			wantErr:    true,
			wantErrMsg: "",
		},
		{
			name:       "invalid phase",
			skipTo:     "invalid-phase",
			wantErr:    true,
			wantErrMsg: "invalid --skip-to value: invalid-phase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := newRootCmd()
			rootCmd.SetArgs([]string{"start", "test-workflow", "test description", "--type", "feature", "--skip-to", tt.skipTo})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStartCmd_WithPlanValidation(t *testing.T) {
	tests := []struct {
		name       string
		skipTo     string
		withPlan   string
		setupFile  bool
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "with-plan without skip-to fails",
			skipTo:     "",
			withPlan:   "plan.json",
			setupFile:  false,
			wantErr:    true,
			wantErrMsg: "--with-plan requires --skip-to to be specified",
		},
		{
			name:       "with-plan with planning phase fails",
			skipTo:     "planning",
			withPlan:   "plan.json",
			setupFile:  false,
			wantErr:    true,
			wantErrMsg: "--with-plan cannot be used when skipping to planning phase",
		},
		{
			name:       "with-plan with non-existent file fails",
			skipTo:     "implementation",
			withPlan:   "/nonexistent/plan.json",
			setupFile:  false,
			wantErr:    true,
			wantErrMsg: "plan file does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var planPath string
			if tt.setupFile {
				tempDir := t.TempDir()
				planPath = filepath.Join(tempDir, "plan.json")
				err := os.WriteFile(planPath, []byte("{}"), 0644)
				require.NoError(t, err)
			} else {
				planPath = tt.withPlan
			}

			rootCmd := newRootCmd()
			args := []string{"start", "test-workflow", "test description", "--type", "feature"}
			if tt.skipTo != "" {
				args = append(args, "--skip-to", tt.skipTo)
			}
			if tt.withPlan != "" {
				args = append(args, "--with-plan", planPath)
			}
			rootCmd.SetArgs(args)

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestResumeCmd_SkipToValidation(t *testing.T) {
	tests := []struct {
		name       string
		skipTo     string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "valid planning phase",
			skipTo:     "planning",
			wantErr:    true,
			wantErrMsg: "",
		},
		{
			name:       "valid implementation phase",
			skipTo:     "implementation",
			wantErr:    true,
			wantErrMsg: "",
		},
		{
			name:       "invalid phase",
			skipTo:     "invalid-phase",
			wantErr:    true,
			wantErrMsg: "invalid --skip-to value: invalid-phase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := newRootCmd()
			rootCmd.SetArgs([]string{"resume", "test-workflow", "--skip-to", tt.skipTo})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
