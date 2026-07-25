package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBacklogPath(t *testing.T) {
	assert.Equal(t, filepath.Join("/x", "TODO.md"), BacklogPath("/x"))
}

func TestReadBacklog(t *testing.T) {
	t.Run("parses checklist lines, ignores prose", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(BacklogPath(tmpDir), []byte(
			"# my backlog\n\n- [ ] open item\n- [x] done item\n  - [X] indented done\nsome prose\n"), 0o644))

		items, err := ReadBacklog(tmpDir)
		require.NoError(t, err)
		require.Len(t, items, 3)
		assert.Equal(t, BacklogItem{Text: "open item", Done: false}, items[0])
		assert.Equal(t, BacklogItem{Text: "done item", Done: true}, items[1])
		assert.Equal(t, BacklogItem{Text: "indented done", Done: true}, items[2])
	})

	t.Run("splits trailing session marker into its own field", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(BacklogPath(tmpDir), []byte(
			"- [ ] fix retries (@fix-auth)\n- [x] host item\n- [ ] weird (@a) (@b)\n"), 0o644))

		items, err := ReadBacklog(tmpDir)
		require.NoError(t, err)
		require.Len(t, items, 3)
		assert.Equal(t, "fix retries", items[0].Text)
		assert.Equal(t, "fix-auth", items[0].Session)
		assert.Equal(t, "host item", items[1].Text)
		assert.Equal(t, "", items[1].Session, "host-added item has no session")
		// Only the last marker is peeled off.
		assert.Equal(t, "weird (@a)", items[2].Text)
		assert.Equal(t, "b", items[2].Session)
	})

	t.Run("missing file yields no items", func(t *testing.T) {
		items, err := ReadBacklog(t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, items)
	})

	t.Run("unreadable path errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(BacklogPath(tmpDir), 0o755)) // dir where a file is expected
		_, err := ReadBacklog(tmpDir)
		require.Error(t, err)
	})
}

func TestEnsureBacklog(t *testing.T) {
	t.Run("creates a file with the convention header and no items", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, EnsureBacklog(tmpDir))
		data, err := os.ReadFile(BacklogPath(tmpDir))
		require.NoError(t, err)
		assert.Contains(t, string(data), "FORGE_SESSION_NAME", "header should mention the session-name env var")
		items, err := ReadBacklog(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, items, "the header is a comment, not a checklist item")
	})

	t.Run("preserves existing content", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(BacklogPath(tmpDir), []byte("- [ ] keep me\n"), 0o644))
		require.NoError(t, EnsureBacklog(tmpDir))
		data, err := os.ReadFile(BacklogPath(tmpDir))
		require.NoError(t, err)
		assert.Equal(t, "- [ ] keep me\n", string(data))
	})

	t.Run("creates the project dir if missing", func(t *testing.T) {
		nested := filepath.Join(t.TempDir(), "-home-user-fresh-project")
		require.NoError(t, EnsureBacklog(nested))
		assert.FileExists(t, BacklogPath(nested))
	})
}

func TestAddBacklogItem(t *testing.T) {
	t.Run("creates file and appends items", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, AddBacklogItem(tmpDir, "first"))
		require.NoError(t, AddBacklogItem(tmpDir, "  second  "))

		items, err := ReadBacklog(tmpDir)
		require.NoError(t, err)
		require.Len(t, items, 2)
		assert.Equal(t, "first", items[0].Text)
		assert.Equal(t, "second", items[1].Text)
		assert.False(t, items[0].Done)
	})

	t.Run("appends after content without a trailing newline", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(BacklogPath(tmpDir), []byte("- [ ] existing"), 0o644))
		require.NoError(t, AddBacklogItem(tmpDir, "new"))
		items, err := ReadBacklog(tmpDir)
		require.NoError(t, err)
		require.Len(t, items, 2)
		assert.Equal(t, "existing", items[0].Text)
		assert.Equal(t, "new", items[1].Text)
	})

	t.Run("rejects empty and multiline text", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.Error(t, AddBacklogItem(tmpDir, "   "))
		require.Error(t, AddBacklogItem(tmpDir, "a\nb"))
	})
}

func TestSetBacklogItemDone(t *testing.T) {
	t.Run("flips the nth item and preserves the rest", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(BacklogPath(tmpDir), []byte(
			"# header\n- [ ] one\n- [ ] two\n  - [ ] three indented\n"), 0o644))

		require.NoError(t, SetBacklogItemDone(tmpDir, 2, true))
		items, err := ReadBacklog(tmpDir)
		require.NoError(t, err)
		assert.False(t, items[0].Done)
		assert.True(t, items[1].Done, "second item marked done")
		assert.False(t, items[2].Done)

		// Indentation and header are preserved; only the checkbox changed.
		data, err := os.ReadFile(BacklogPath(tmpDir))
		require.NoError(t, err)
		assert.Contains(t, string(data), "# header\n")
		assert.Contains(t, string(data), "  - [ ] three indented")

		// Reopen it.
		require.NoError(t, SetBacklogItemDone(tmpDir, 2, false))
		items, err = ReadBacklog(tmpDir)
		require.NoError(t, err)
		assert.False(t, items[1].Done)
	})

	t.Run("errors on out-of-range and missing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.Error(t, SetBacklogItemDone(tmpDir, 0, true))
		require.Error(t, SetBacklogItemDone(tmpDir, 1, true)) // no file yet

		require.NoError(t, AddBacklogItem(tmpDir, "only one"))
		require.Error(t, SetBacklogItemDone(tmpDir, 5, true))
	})
}
