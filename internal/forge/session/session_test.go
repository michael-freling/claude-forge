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
				"-work/session-1.jsonl": `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}
{"type":"human","timestamp":"2026-05-08T14:30:01Z","message":"Hello world"}
{"type":"assistant","timestamp":"2026-05-08T14:30:02Z","message":"Hi there"}`,
				"-work/session-2.jsonl": `{"type":"system","timestamp":"2026-05-09T10:00:00Z","message":"init"}
{"type":"human","timestamp":"2026-05-09T10:00:01Z","message":"Fix the bug"}`,
			},
			want: []Session{
				{
					ID:        "session-2",
					CreatedAt: time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC),
					FirstMsg:  "Fix the bug",
					Subdir:    "-work",
				},
				{
					ID:        "session-1",
					CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
					FirstMsg:  "Hello world",
					Subdir:    "-work",
				},
			},
		},
		{
			name: "sessions from worktree subdirs are also surfaced",
			files: map[string]string{
				"-work/main.jsonl": `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}
{"type":"human","timestamp":"2026-05-08T14:30:01Z","message":"main work"}`,
				"-work-.claude-worktrees-feature/wt.jsonl": `{"type":"system","timestamp":"2026-05-10T09:00:00Z","message":"init"}
{"type":"human","timestamp":"2026-05-10T09:00:01Z","message":"worktree work"}`,
			},
			want: []Session{
				{
					ID:        "wt",
					CreatedAt: time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC),
					FirstMsg:  "worktree work",
					Subdir:    "-work-.claude-worktrees-feature",
				},
				{
					ID:        "main",
					CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
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
			name: "session without human message",
			files: map[string]string{
				"session-1.jsonl": `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}`,
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
				"session-1.jsonl": `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}`,
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
				"bad.jsonl":  `{"type":"system","message":"no timestamp"}`,
				"good.jsonl": `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}`,
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

	// File with invalid JSON on some lines but valid data
	content := `not json at all
{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}
also not json
{"type":"human","timestamp":"2026-05-08T14:30:01Z","message":"Hello"}
`
	err := os.WriteFile(filepath.Join(tmpDir, "session-1.jsonl"), []byte(content), 0o644)
	require.NoError(t, err)

	got, err := List(tmpDir)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "session-1", got[0].ID)
	assert.Equal(t, "Hello", got[0].FirstMsg)
	assert.Equal(t, time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC), got[0].CreatedAt)
}

func TestSession_IsWorktree(t *testing.T) {
	tests := []struct {
		subdir string
		want   bool
	}{
		{"-work", false},
		{"", false},
		{"-work-.claude-worktrees-feature", true},
		{"-work-.claude-worktrees-my-long-branch-name", true},
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
		{"-work-.claude-worktrees-feature", "feature"},
		{"-work-.claude-worktrees-my-long-branch-name", "my-long-branch-name"},
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
		[]byte(`{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}`+"\n"+
			`{"type":"human","timestamp":"2026-05-08T14:30:01Z","message":"hello"}`+"\n"), 0o644))

	wtDir := filepath.Join(tmpDir, "-work-.claude-worktrees-feature")
	require.NoError(t, os.MkdirAll(wtDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "wt-session.jsonl"),
		[]byte(`{"type":"system","timestamp":"2026-05-09T10:00:00Z","message":"init"}`+"\n"+
			`{"type":"human","timestamp":"2026-05-09T10:00:01Z","message":"worktree hello"}`+"\n"), 0o644))

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
		assert.Equal(t, "-work-.claude-worktrees-feature", sess.Subdir)
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
			name: "valid session with system and human messages",
			content: `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}
{"type":"human","timestamp":"2026-05-08T14:30:01Z","message":"Hello world"}`,
			want: &Session{
				ID:        "test-session",
				CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
				FirstMsg:  "Hello world",
			},
		},
		{
			name:    "system message only",
			content: `{"type":"system","timestamp":"2026-05-08T14:30:00Z","message":"init"}`,
			want: &Session{
				ID:        "test-session",
				CreatedAt: time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC),
				FirstMsg:  "",
			},
		},
		{
			name:        "no timestamp at all",
			content:     `{"type":"system","message":"init"}`,
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
