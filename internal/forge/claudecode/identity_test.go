package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestWriteContainerIdentity_UnchangedIsNotRewritten(t *testing.T) {
	configDir := t.TempDir()
	id, err := WriteContainerIdentity(configDir, 1000, 1000)
	require.NoError(t, err)

	// Backdate the files so a rewrite would visibly change their mtime.
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	before := map[string]os.FileInfo{}
	for _, p := range []string{id.PasswdFile, id.GroupFile} {
		require.NoError(t, os.Chtimes(p, past, past))
		info, err := os.Stat(p)
		require.NoError(t, err)
		before[p] = info
	}

	_, err = WriteContainerIdentity(configDir, 1000, 1000)
	require.NoError(t, err)
	for p, old := range before {
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.True(t, os.SameFile(old, info), "%s was replaced", p)
		assert.Equal(t, old.ModTime(), info.ModTime(), "%s was rewritten", p)
	}

	entries, err := os.ReadDir(configDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "no temporary files are left behind")
}

func TestWriteFileAtomic(t *testing.T) {
	tests := []struct {
		name     string
		existing *string
		data     string
	}{
		{name: "new file", data: "new\n"},
		{name: "replaces different content", existing: stringPtr("old\n"), data: "new\n"},
		{name: "replaces empty file", existing: stringPtr(""), data: "new\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file")
			if tt.existing != nil {
				require.NoError(t, os.WriteFile(path, []byte(*tt.existing), 0o600))
			}

			require.NoError(t, writeFileAtomic(path, []byte(tt.data)))

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.data, string(data))
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Len(t, entries, 1, "no temporary files are left behind")
		})
	}
}

func TestWriteFileAtomic_MissingDir(t *testing.T) {
	assert.Error(t, writeFileAtomic(filepath.Join(t.TempDir(), "missing", "file"), []byte("x")))
}

// TestWriteContainerIdentity_Concurrent runs writers for two identities
// alongside readers: a reader must only ever see one complete file, never an
// empty or partial one.
func TestWriteContainerIdentity_Concurrent(t *testing.T) {
	configDir := t.TempDir()
	_, err := WriteContainerIdentity(configDir, 1000, 1000)
	require.NoError(t, err)

	wantPasswd := map[string]bool{}
	wantGroup := map[string]bool{}
	for _, id := range []int{1000, 2000} {
		wantPasswd[RenderContainerPasswd(id, id)] = true
		wantGroup[RenderContainerGroup(id)] = true
	}

	done := make(chan struct{})
	errs := make(chan error, 8)
	var writers, readers sync.WaitGroup
	for w := range 4 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			id := 1000 + 1000*(w%2)
			for range 50 {
				if _, err := WriteContainerIdentity(configDir, id, id); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	for file, want := range map[string]map[string]bool{
		ContainerPasswdFile: wantPasswd,
		ContainerGroupFile:  wantGroup,
	} {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				data, err := os.ReadFile(filepath.Join(configDir, file))
				if err != nil {
					errs <- err
					return
				}
				if !want[string(data)] {
					errs <- fmt.Errorf("%s had unexpected content %q", file, data)
					return
				}
			}
		}()
	}

	writers.Wait()
	close(done)
	readers.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}
}

func stringPtr(s string) *string { return &s }
