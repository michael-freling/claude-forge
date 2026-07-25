package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/michael-freling/claude-forge/internal/forge/session"
	"github.com/spf13/cobra"
)

// backlogEditor opens the shared backlog file in the user's editor. It is a
// package var so tests can substitute a non-interactive editor.
var backlogEditor = func(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	// EDITOR may carry arguments (e.g. "code -w"); split on spaces.
	fields := strings.Fields(editor)
	args := append(fields[1:], path)
	c := exec.Command(fields[0], args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// newTodosAddCmd adds an item to the current project's shared backlog.
func newTodosAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <text>",
		Short: "Add an item to the current project's shared backlog",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionDir, err := projectSessionDir()
			if err != nil {
				return err
			}
			text := strings.Join(args, " ")
			if err := session.AddBacklogItem(sessionDir, text); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added: %s\n", text)
			return nil
		},
	}
}

// newTodosDoneCmd checks off (or reopens) a backlog item in the current project.
func newTodosDoneCmd() *cobra.Command {
	var reopen bool
	cmd := &cobra.Command{
		Use:   "done <n>",
		Short: "Mark backlog item <n> done (or reopen it) in the current project",
		Long: `Marks the n-th item of the current project's shared backlog as done, where
n is the number shown by 'claude-forge todos'. Pass --reopen to unmark it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid item number %q", args[0])
			}
			sessionDir, err := projectSessionDir()
			if err != nil {
				return err
			}
			if err := session.SetBacklogItemDone(sessionDir, n, !reopen); err != nil {
				return err
			}
			verb := "Marked done"
			if reopen {
				verb = "Reopened"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: item %d\n", verb, n)
			return nil
		},
	}
	cmd.Flags().BoolVar(&reopen, "reopen", false, "Reopen the item instead of marking it done")
	return cmd
}

// newTodosEditCmd opens the current project's shared backlog in $EDITOR.
func newTodosEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the current project's shared backlog in $EDITOR",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionDir, err := projectSessionDir()
			if err != nil {
				return err
			}
			if err := session.EnsureBacklog(sessionDir); err != nil {
				return err
			}
			return backlogEditor(session.BacklogPath(sessionDir))
		},
	}
}
