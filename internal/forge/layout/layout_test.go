package layout

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPaths(t *testing.T) {
	assert.Equal(t, "/home/user/work", Workspace)
	assert.Equal(t, "/home/user/.claude/projects", ProjectsDir)
	assert.Equal(t, "/etc/claude-code/CLAUDE.md", ManagedMemory)

	// The workspace must stay inside the home directory: the agent runs as that
	// user, and the entrypoint's ownership fixup covers that subtree.
	assert.Contains(t, Workspace, Home+"/")

	// Claude Code never reads a CLAUDE.md at the filesystem root.
	assert.NotEqual(t, "/CLAUDE.md", ManagedMemory)
}

func TestEncodePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/work", "-home-user-work"},
		{"/home/user/work/.claude/worktrees/feature", "-home-user-work--claude-worktrees-feature"},
		{"/work", "-work"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, EncodePath(tt.path), "EncodePath(%q)", tt.path)
	}
}

func TestSessionSubdirs(t *testing.T) {
	assert.Equal(t, "-home-user-work", SessionSubdir)
	assert.Equal(t, "-home-user-work--claude-worktrees-", WorktreeSubdirPrefix)

	// A worktree bucket is the encoding of the worktree's own path, so the
	// prefix and WorktreePath must agree.
	assert.Equal(t, WorktreeSubdirPrefix+"feature", EncodePath(WorktreePath("feature")))

}

func TestWorktreePath(t *testing.T) {
	assert.Equal(t, "/home/user/work/.claude/worktrees/my-feature", WorktreePath("my-feature"))
}
