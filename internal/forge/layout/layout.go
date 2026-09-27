// Package layout describes the filesystem layout inside the agent container:
// where the host project is mounted, where Claude Code keeps its own state, and
// the session bucket names that follow from the working directory.
//
// The Docker wrapper (mount targets, working directory), the Claude Code config
// writers (the project key in .claude.json) and the session lister (bucket
// names) all depend on the same paths, so they live here in one dependency-free
// package: changing Workspace changes all of them together instead of leaving a
// literal behind in one of them.
package layout

import "strings"

const (
	// Home is the container home directory of the user the agent runs as.
	Home = "/home/user"

	// Workspace is where the host project directory is bind-mounted, and the
	// agent's working directory.
	//
	// It lives under Home so everything a session touches sits in one subtree.
	// It also determines SessionSubdir below, because Claude Code names a
	// session bucket after its working directory — moving Workspace moves where
	// new transcripts land on the host.
	Workspace = Home + "/work"

	// ProjectsDir is where Claude Code stores session transcripts, one bucket
	// per working directory. claude-forge bind-mounts the host's
	// ~/.claude-forge/<project-id>/ here so every bucket persists.
	ProjectsDir = Home + "/.claude/projects"

	// WorktreesSubdir is where Claude Code's --worktree creates worktrees,
	// relative to Workspace.
	WorktreesSubdir = ".claude/worktrees"

	// ManagedMemory is the CLAUDE.md that Claude Code loads as managed memory
	// on Linux — ahead of user memory (~/.claude/CLAUDE.md) and project memory
	// (<Workspace>/CLAUDE.md), on every run, and not suppressible via the
	// claudeMdExcludes setting.
	//
	// Claude Code discovers project memory by walking up from the working
	// directory only while the directory differs from the filesystem root, so a
	// file at /CLAUDE.md is silently ignored; this is the injection point that
	// works from outside the project tree.
	ManagedMemory = "/etc/claude-code/CLAUDE.md"

	// LegacyWorktreeSubdirPrefix is the worktree bucket prefix Claude Code
	// produced while the workspace was mounted at /work. Sessions recorded then
	// still sit in the host session directory, so the session lister keeps
	// recognizing it.
	LegacyWorktreeSubdirPrefix = "-work--claude-worktrees-"
)

var (
	// SessionSubdir is the bucket Claude Code writes transcripts to when its
	// working directory is Workspace.
	SessionSubdir = EncodePath(Workspace)

	// WorktreeSubdirPrefix is the bucket prefix for worktree sessions; the
	// worktree name follows it.
	WorktreeSubdirPrefix = EncodePath(Workspace+"/"+WorktreesSubdir) + "-"
)

// EncodePath renders an absolute container path the way Claude Code names the
// session bucket for a working directory: every "/" and "." becomes "-".
func EncodePath(path string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(path)
}

// WorktreePath returns the path of a Claude Code worktree inside the workspace.
func WorktreePath(name string) string {
	return Workspace + "/" + WorktreesSubdir + "/" + name
}
