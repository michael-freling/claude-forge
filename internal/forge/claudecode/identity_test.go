package claudecode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderContainerPasswd(t *testing.T) {
	got := RenderContainerPasswd(1234, 5678)
	assert.Contains(t, got, "user:x:1234:5678::/home/user:/bin/bash\n")
	assert.Contains(t, got, "root:x:0:0:")
}

func TestRenderContainerGroup(t *testing.T) {
	got := RenderContainerGroup(5678)
	assert.Contains(t, got, "user:x:5678:\n")
	assert.Contains(t, got, "root:x:0:\n")
}

func TestWriteContainerIdentity(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "claude-forge")

	id, err := WriteContainerIdentity(configDir, 1000, 1001)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(configDir, ContainerPasswdFile), id.PasswdFile)
	assert.Equal(t, filepath.Join(configDir, ContainerGroupFile), id.GroupFile)

	data, err := os.ReadFile(id.PasswdFile)
	require.NoError(t, err)
	assert.Equal(t, RenderContainerPasswd(1000, 1001), string(data))
	data, err = os.ReadFile(id.GroupFile)
	require.NoError(t, err)
	assert.Equal(t, RenderContainerGroup(1001), string(data))
}

func TestWriteContainerIdentity_RejectsRoot(t *testing.T) {
	for _, tt := range []struct{ uid, gid int }{{0, 1000}, {1000, 0}, {0, 0}, {-1, 1000}} {
		_, err := WriteContainerIdentity(t.TempDir(), tt.uid, tt.gid)
		assert.Error(t, err, "uid=%d gid=%d", tt.uid, tt.gid)
	}
}

func TestWriteContainerIdentity_ConfigDirError(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(parent, []byte("x"), 0o644))

	_, err := WriteContainerIdentity(filepath.Join(parent, "sub"), 1000, 1000)
	assert.Error(t, err)
}

func TestWriteContainerIdentity_WriteError(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(configDir, ContainerPasswdFile), 0o755))
	_, err := WriteContainerIdentity(configDir, 1000, 1000)
	assert.ErrorContains(t, err, ContainerPasswdFile)

	configDir = t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(configDir, ContainerGroupFile), 0o755))
	_, err = WriteContainerIdentity(configDir, 1000, 1000)
	assert.ErrorContains(t, err, ContainerGroupFile)
}
