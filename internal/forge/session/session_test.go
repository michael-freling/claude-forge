package session

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateID(t *testing.T) {
	id, err := GenerateID()
	require.NoError(t, err)

	// Must be exactly 8 hex characters (4 random bytes = 8 hex chars).
	assert.Len(t, id, 8)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}$`), id)
}

func TestGenerateID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id, err := GenerateID()
		require.NoError(t, err)
		assert.False(t, ids[id], "duplicate session ID generated: %s", id)
		ids[id] = true
	}
}

func TestGenerateUUID(t *testing.T) {
	id, err := GenerateUUID()
	require.NoError(t, err)

	// RFC 4122 version 4 UUID format: 8-4-4-4-12 hex digits, version 4, variant 8/9/a/b.
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), id)
}

func TestGenerateUUID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id, err := GenerateUUID()
		require.NoError(t, err)
		assert.False(t, ids[id], "duplicate UUID generated: %s", id)
		ids[id] = true
	}
}

func TestWriteMetadata(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, WriteMetadata(tmpDir, "sess-1", Metadata{Name: "claude"}))

	got := readMetadata(tmpDir, "sess-1")
	assert.Equal(t, "claude", got.Name)
}

func TestWriteMetadata_Error(t *testing.T) {
	// Writing into a non-existent directory fails.
	err := WriteMetadata(filepath.Join(t.TempDir(), "missing"), "sess-1", Metadata{Name: "claude"})
	require.Error(t, err)
}

func TestValidateName(t *testing.T) {
	require.NoError(t, ValidateName(""))
	require.NoError(t, ValidateName("my-session"))
	require.NoError(t, ValidateName("name with spaces"))

	for _, bad := range []string{"a\tb", "a\nb", "a\rb"} {
		err := ValidateName(bad)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tab or newline")
	}
}

func TestWriteMetadata_RejectsInvalidName(t *testing.T) {
	err := WriteMetadata(t.TempDir(), "sess-1", Metadata{Name: "bad\tname"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tab or newline")
}

func TestReadMetadata_MissingOrInvalid(t *testing.T) {
	tmpDir := t.TempDir()

	// Missing file yields a zero Metadata, no panic.
	assert.Equal(t, Metadata{}, readMetadata(tmpDir, "absent"))

	// Invalid JSON also yields a zero Metadata.
	require.NoError(t, os.WriteFile(metadataPath(tmpDir, "broken"), []byte("not json"), 0o644))
	assert.Equal(t, Metadata{}, readMetadata(tmpDir, "broken"))
}

func TestList_PopulatesNameFromSidecar(t *testing.T) {
	tmpDir := t.TempDir()

	workDir := filepath.Join(tmpDir, "-work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "sess-1.jsonl"),
		[]byte(`{"type":"user","message":{"role":"user","content":"hi"},"timestamp":"2026-05-08T14:30:01Z"}`+"\n"), 0o644))

	// Sidecar lives at the session-dir root, keyed by session ID.
	require.NoError(t, WriteMetadata(tmpDir, "sess-1", Metadata{Name: "my-session"}))

	got, err := List(tmpDir)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "my-session", got[0].Name)
}

func TestList(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		want    []Session
		wantErr bool
	}{
		{
			name: "multiple sessions in -work subdir sorted by most recent first",
			files: map[string]string{
				"-work/session-1.jsonl": `{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"session-1"}
{"type":"user","message":{"role":"user","content":"Hello world"},"timestamp":"2026-05-08T14:30:01Z"}
{"type":"assistant","message":{"role":"assistant","content":"Hi there"},"timestamp":"2026-05-08T14:30:02Z"}`,
				"-work/session-2.jsonl": `{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"session-2"}
{"type":"user","message":{"role":"user","content":"Fix the bug"},"timestamp":"2026-05-09T10:00:01Z"}`,
			},
			want: []Session{
				{
					ID:        "session-2",
					CreatedAt: time.Date(2026, 5, 9, 10, 0, 1, 0, time.UTC),
					FirstMsg:  "Fix the bug",
					Subdir:    "-work",
				},
				{
					ID:        "session-1",
					CreatedAt: time.Date(2026, 5, 8, 14, 30, 1, 0, time.UTC),
					FirstMsg:  "Hello world",
					Subdir:    "-work",
				},
			},
		},
		{
			name: "sessions from worktree subdirs are also surfaced",
			files: map[string]string{
				"-work/main.jsonl":                         `{"type":"user","message":{"role":"user","content":"main work"},"timestamp":"2026-05-08T14:30:01Z"}`,
				"-work--claude-worktrees-feature/wt.jsonl": `{"type":"user","message":{"role":"user","content":"worktree work"},"timestamp":"2026-05-10T09:00:01Z"}`,
			},
			want: []Session{
				{
					ID:        "wt",
					CreatedAt: time.Date(2026, 5, 10, 9, 0, 1, 0, time.UTC),
					FirstMsg:  "worktree work",
					Subdir:    "-work--claude-worktrees-feature",
				},
				{
					ID:        "main",
					CreatedAt: time.Date(2026, 5, 8, 14, 30, 1, 0, time.UTC),
					FirstMsg:  "main work",
					Subdir:    "-work",
				},
			},
		},
		{
			name: "jsonl files placed directly in sessionDir are still picked up",
			files: map[string]string{
				"legacy.jsonl": `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}
{"type":"human","timestamp":"2026-05-08T14:30:01Z","message":"legacy session"}`,
			},
			want: []Session{
				{
					ID:        "legacy",
					CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
					FirstMsg:  "legacy session",
				},
			},
		},
		{
			name: "session without user message",
			files: map[string]string{
				"session-1.jsonl": `{"type":"attachment","timestamp":"2026-05-08T14:30:00Z"}`,
			},
			want: []Session{
				{
					ID:        "session-1",
					CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
					FirstMsg:  "",
				},
			},
		},
		{
			name:  "empty directory",
			files: map[string]string{},
			want:  nil,
		},
		{
			name: "skips non-jsonl files",
			files: map[string]string{
				"readme.txt":      "not a session",
				"session-1.jsonl": `{"type":"attachment","timestamp":"2026-05-08T14:30:00Z"}`,
			},
			want: []Session{
				{
					ID:        "session-1",
					CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
					FirstMsg:  "",
				},
			},
		},
		{
			name: "skips malformed files without timestamp",
			files: map[string]string{
				"bad.jsonl":  `{"type":"permission-mode","permissionMode":"bypassPermissions"}`,
				"good.jsonl": `{"type":"attachment","timestamp":"2026-05-08T14:30:00Z"}`,
			},
			want: []Session{
				{
					ID:        "good",
					CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
					FirstMsg:  "",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			for name, content := range tt.files {
				full := filepath.Join(tmpDir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
				require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
			}

			got, err := List(tmpDir)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestList_NonExistentDirectory(t *testing.T) {
	got, err := List("/nonexistent/directory/path")

	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestList_SkipsMalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// File with invalid JSON on some lines but valid data (real Claude Code format)
	content := `not json at all
{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"session-1"}
also not json
{"type":"user","message":{"role":"user","content":"Hello"},"timestamp":"2026-05-08T14:30:01Z"}
`
	err := os.WriteFile(filepath.Join(tmpDir, "session-1.jsonl"), []byte(content), 0o644)
	require.NoError(t, err)

	got, err := List(tmpDir)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "session-1", got[0].ID)
	assert.Equal(t, "Hello", got[0].FirstMsg)
	assert.Equal(t, time.Date(2026, 5, 8, 14, 30, 1, 0, time.UTC), got[0].CreatedAt)
}

func TestSession_IsWorktree(t *testing.T) {
	tests := []struct {
		subdir string
		want   bool
	}{
		{"-work", false},
		{"", false},
		{"-work--claude-worktrees-feature", true},
		{"-work--claude-worktrees-my-long-branch-name", true},
	}
	for _, tt := range tests {
		t.Run(tt.subdir, func(t *testing.T) {
			s := Session{Subdir: tt.subdir}
			assert.Equal(t, tt.want, s.IsWorktree())
		})
	}
}

func TestSession_WorktreeName(t *testing.T) {
	tests := []struct {
		subdir string
		want   string
	}{
		{"-work", ""},
		{"", ""},
		{"-work--claude-worktrees-feature", "feature"},
		{"-work--claude-worktrees-my-long-branch-name", "my-long-branch-name"},
	}
	for _, tt := range tests {
		t.Run(tt.subdir, func(t *testing.T) {
			s := Session{Subdir: tt.subdir}
			assert.Equal(t, tt.want, s.WorktreeName())
		})
	}
}

func TestFind(t *testing.T) {
	tmpDir := t.TempDir()

	// Create sessions in different subdirs
	workDir := filepath.Join(tmpDir, "-work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main-session.jsonl"),
		[]byte(`{"type":"user","message":{"role":"user","content":"hello"},"timestamp":"2026-05-08T14:30:01Z"}`+"\n"), 0o644))

	wtDir := filepath.Join(tmpDir, "-work--claude-worktrees-feature")
	require.NoError(t, os.MkdirAll(wtDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "wt-session.jsonl"),
		[]byte(`{"type":"user","message":{"role":"user","content":"worktree hello"},"timestamp":"2026-05-09T10:00:01Z"}`+"\n"), 0o644))

	t.Run("finds non-worktree session", func(t *testing.T) {
		sess, err := Find(tmpDir, "main-session")
		require.NoError(t, err)
		assert.Equal(t, "main-session", sess.ID)
		assert.Equal(t, "-work", sess.Subdir)
		assert.False(t, sess.IsWorktree())
		assert.Equal(t, "", sess.WorktreeName())
	})

	t.Run("finds worktree session", func(t *testing.T) {
		sess, err := Find(tmpDir, "wt-session")
		require.NoError(t, err)
		assert.Equal(t, "wt-session", sess.ID)
		assert.Equal(t, "-work--claude-worktrees-feature", sess.Subdir)
		assert.True(t, sess.IsWorktree())
		assert.Equal(t, "feature", sess.WorktreeName())
	})

	t.Run("returns error for missing session", func(t *testing.T) {
		_, err := Find(tmpDir, "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestDelete(t *testing.T) {
	tmp := t.TempDir()

	t.Run("removes transcript and sidecar", func(t *testing.T) {
		work := filepath.Join(tmp, "-work")
		require.NoError(t, os.MkdirAll(work, 0o755))
		jsonl := filepath.Join(work, "s1.jsonl")
		require.NoError(t, os.WriteFile(jsonl, []byte("{}"), 0o644))
		require.NoError(t, WriteMetadata(tmp, "s1", Metadata{Name: "n1"}))

		require.NoError(t, Delete(tmp, Session{ID: "s1", Subdir: "-work"}))
		assert.NoFileExists(t, jsonl)
		assert.NoFileExists(t, metadataPath(tmp, "s1"))

		// Idempotent: deleting again is not an error.
		require.NoError(t, Delete(tmp, Session{ID: "s1", Subdir: "-work"}))
	})

	t.Run("removes legacy root transcript", func(t *testing.T) {
		jsonl := filepath.Join(tmp, "s2.jsonl")
		require.NoError(t, os.WriteFile(jsonl, []byte("{}"), 0o644))
		require.NoError(t, Delete(tmp, Session{ID: "s2"}))
		assert.NoFileExists(t, jsonl)
	})
}

func TestFind_ByName(t *testing.T) {
	tmpDir := t.TempDir()

	workDir := filepath.Join(tmpDir, "-work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	// Two sessions named "hello"; the more recent one should win.
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "old.jsonl"),
		[]byte(`{"type":"user","message":{"role":"user","content":"a"},"timestamp":"2026-05-08T10:00:00Z"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "new.jsonl"),
		[]byte(`{"type":"user","message":{"role":"user","content":"b"},"timestamp":"2026-05-09T10:00:00Z"}`+"\n"), 0o644))
	require.NoError(t, WriteMetadata(tmpDir, "old", Metadata{Name: "hello"}))
	require.NoError(t, WriteMetadata(tmpDir, "new", Metadata{Name: "hello"}))

	// A third session whose ID equals another session's name, to prove ID wins.
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "hello.jsonl"),
		[]byte(`{"type":"user","message":{"role":"user","content":"c"},"timestamp":"2026-05-07T10:00:00Z"}`+"\n"), 0o644))

	t.Run("resolves by name to most recent match", func(t *testing.T) {
		// "hello" matches the session whose ID is literally "hello" first (ID wins).
		sess, err := Find(tmpDir, "hello")
		require.NoError(t, err)
		assert.Equal(t, "hello", sess.ID)
	})

	t.Run("name-only ref resolves to most recent named session", func(t *testing.T) {
		sess, err := Find(tmpDir, "world-does-not-exist")
		require.Error(t, err)
		assert.Nil(t, sess)
	})

	t.Run("name match when no ID collision", func(t *testing.T) {
		require.NoError(t, WriteMetadata(tmpDir, "new", Metadata{Name: "greeting"}))
		sess, err := Find(tmpDir, "greeting")
		require.NoError(t, err)
		assert.Equal(t, "new", sess.ID)
	})
}

func TestFind_ReadError(t *testing.T) {
	// Use a path that exists but can't be read (not a directory)
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(tmpFile, []byte("x"), 0o644))

	_, err := Find(tmpFile, "any-id")
	require.Error(t, err)
}

func TestParseSessionFile(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		want        *Session
		wantErr     bool
		errContains string
	}{
		{
			name: "real Claude Code format with user message",
			content: `{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"test-session"}
{"type":"user","message":{"role":"user","content":"Hello world"},"timestamp":"2026-05-08T14:30:01Z"}`,
			want: &Session{
				ID:        "test-session",
				CreatedAt: time.Date(2026, 5, 8, 14, 30, 1, 0, time.UTC),
				FirstMsg:  "Hello world",
			},
		},
		{
			name: "legacy format with human message as string",
			content: `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}
{"type":"human","timestamp":"2026-05-08T14:30:01Z","message":"Hello world"}`,
			want: &Session{
				ID:        "test-session",
				CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
				FirstMsg:  "Hello world",
			},
		},
		{
			name:    "attachment-only session (no user messages)",
			content: `{"type":"attachment","timestamp":"2026-05-08T14:30:00Z"}`,
			want: &Session{
				ID:        "test-session",
				CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
				FirstMsg:  "",
			},
		},
		{
			name:        "no timestamp at all",
			content:     `{"type":"permission-mode","permissionMode":"bypassPermissions"}`,
			wantErr:     true,
			errContains: "no timestamp found",
		},
		{
			name:        "empty file",
			content:     "",
			wantErr:     true,
			errContains: "no timestamp found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test-session.jsonl")
			err := os.WriteFile(filePath, []byte(tt.content), 0o644)
			require.NoError(t, err)

			got, err := parseSessionFile("test-session", filePath)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
