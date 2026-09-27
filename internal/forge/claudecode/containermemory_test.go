package claudecode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michael-freling/claude-forge/internal/forge/layout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerMemoryTarget(t *testing.T) {
	// Claude Code's upward memory walk from the cwd stops before the filesystem
	// root, so /CLAUDE.md would never be read; the managed-settings directory is
	// the path that is always loaded on Linux.
	assert.Equal(t, "/etc/claude-code/CLAUDE.md", ContainerMemoryTarget)
	assert.Equal(t, layout.ManagedMemory, ContainerMemoryTarget)
	assert.NotEqual(t, "/CLAUDE.md", ContainerMemoryTarget)
}

func TestDefaultContainerMemory(t *testing.T) {
	content := DefaultContainerMemory()

	// The two things the agent cannot work out for itself.
	assert.Contains(t, content, "inside a claude-forge container, not on the user's machine",
		"must state that the session runs in a container, not on the host")
	assert.Contains(t, content, "bind-mounted at `"+layout.Workspace+"`",
		"must state that the host project directory is mounted at the workspace")
	assert.Contains(t, content, "write it relative to `"+layout.Workspace+"`",
		"must require paths shared with the user to be relative to the workspace")
	assert.Contains(t, content, "`"+layout.Workspace+"/internal/forge/orchestrator.go`",
		"must show the container-absolute form that is not to be used")
	assert.NotContains(t, content, workspacePlaceholder,
		"the workspace placeholder must be rendered, not shipped verbatim")

	// Tool calls still need real container paths; the rule is about what the
	// user is shown.
	assert.Contains(t, content, "Tool calls still take real container")
}

func TestWriteContainerMemory(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "nested", "config")

	path, err := WriteContainerMemory(configDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(configDir, "container-CLAUDE.md"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, DefaultContainerMemory(), string(data))

	// Readable inside the container, where the agent runs as a different user.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestWriteContainerMemory_OverwritesStaleContent(t *testing.T) {
	configDir := t.TempDir()
	stale := filepath.Join(configDir, ContainerMemoryFile)
	require.NoError(t, os.WriteFile(stale, []byte("# from an older claude-forge\n"), 0o644))

	path, err := WriteContainerMemory(configDir)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, DefaultContainerMemory(), string(data),
		"the file is forge-managed and must be regenerated, not preserved")
}

func TestWriteContainerMemory_DirectoryCreationError(t *testing.T) {
	baseDir := t.TempDir()
	blockingFile := filepath.Join(baseDir, "blocked")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o644))

	_, err := WriteContainerMemory(filepath.Join(blockingFile, "config"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create config directory")
}

func TestWriteContainerMemory_WriteFileError(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.Chmod(configDir, 0o555))
	t.Cleanup(func() { os.Chmod(configDir, 0o755) })

	_, err := WriteContainerMemory(configDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write container-CLAUDE.md")
}
