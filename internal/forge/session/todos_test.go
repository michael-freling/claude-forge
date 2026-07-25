package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	todoSessionA = "11111111-2222-4333-8444-555555555555"
	todoSessionB = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	todoAgentX   = "99999999-8888-4777-8666-555555555555"
)

// writeTodoFile writes a todo file under the project dir's todos subdir.
func writeTodoFile(t *testing.T, sessionDir, name, content string) string {
	t.Helper()
	dir := TodosDir(sessionDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// writeTaskFile writes one task item under the project dir's tasks subdir,
// using the tasks/<session-id>/<n>.json layout.
func writeTaskFile(t *testing.T, sessionDir, sessionID, name, content string) string {
	t.Helper()
	dir := filepath.Join(TasksDir(sessionDir), sessionID)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestTodosDir(t *testing.T) {
	assert.Equal(t, filepath.Join("/x", "todos"), TodosDir("/x"))
}

func TestTodoList_Open(t *testing.T) {
	l := TodoList{Todos: []Todo{
		{Content: "a", Status: "pending"},
		{Content: "b", Status: "in_progress"},
		{Content: "c", Status: "completed"},
	}}
	assert.Equal(t, 2, l.Open())
	assert.Equal(t, 0, TodoList{}.Open())
}

func TestTodoFileSessionID(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		wantID   string
		wantMain bool
	}{
		{"main thread", todoSessionA + "-agent-" + todoSessionA + ".json", todoSessionA, true},
		{"subagent", todoSessionA + "-agent-" + todoAgentX + ".json", todoSessionA, false},
		{"legacy bare uuid", todoSessionA + ".json", todoSessionA, true},
		{"not json", todoSessionA + ".txt", "", false},
		{"not a uuid", "settings.json", "", false},
		{"agent form with non-uuid session", "sess-1-agent-" + todoAgentX + ".json", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, main := todoFileSessionID(tt.file)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantMain, main)
		})
	}
}

func TestListTodos(t *testing.T) {
	t.Run("reads main-thread lists sorted by mtime desc", func(t *testing.T) {
		tmpDir := t.TempDir()
		older := writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoSessionA+".json",
			`[{"content":"write tests","status":"pending","activeForm":"Writing tests"}]`)
		old := time.Now().Add(-2 * time.Hour)
		require.NoError(t, os.Chtimes(older, old, old))
		writeTodoFile(t, tmpDir, todoSessionB+"-agent-"+todoSessionB+".json",
			`[{"content":"ship it","status":"completed"},{"content":"fix bug","status":"in_progress"}]`)

		got, err := ListTodos(tmpDir)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, todoSessionB, got[0].SessionID, "most recently updated first")
		assert.Equal(t, todoSessionA, got[1].SessionID)
		assert.Equal(t, "write tests", got[1].Todos[0].Content)
		assert.Equal(t, "Writing tests", got[1].Todos[0].ActiveForm)
		assert.Equal(t, 1, got[0].Open())
	})

	t.Run("skips subagent, empty, and unparseable lists", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoAgentX+".json", `[{"content":"sub","status":"pending"}]`)
		writeTodoFile(t, tmpDir, todoSessionB+"-agent-"+todoSessionB+".json", `[]`)
		writeTodoFile(t, tmpDir, todoAgentX+".json", `not json`)
		require.NoError(t, os.MkdirAll(filepath.Join(TodosDir(tmpDir), "subdir"), 0o755))

		got, err := ListTodos(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("missing todos dir yields none", func(t *testing.T) {
		got, err := ListTodos(t.TempDir())
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("unreadable todos dir errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(TodosDir(tmpDir), []byte("x"), 0o644))
		_, err := ListTodos(tmpDir)
		require.Error(t, err)
	})

	t.Run("reads task layout ordered by file number", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Numeric order, not lexicographic: 10 must sort after 2.
		writeTaskFile(t, tmpDir, todoSessionA, "10.json", `{"id":"10","subject":"last","status":"pending","activeForm":"Lasting"}`)
		writeTaskFile(t, tmpDir, todoSessionA, "1.json", `{"id":"1","subject":"first","status":"completed"}`)
		writeTaskFile(t, tmpDir, todoSessionA, "2.json", `{"id":"2","subject":"second","status":"in_progress"}`)
		writeTaskFile(t, tmpDir, todoSessionA, ".lock", ``)

		got, err := ListTodos(tmpDir)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, todoSessionA, got[0].SessionID)
		require.Len(t, got[0].Todos, 3)
		assert.Equal(t, []Todo{
			{Content: "first", Status: "completed"},
			{Content: "second", Status: "in_progress"},
			{Content: "last", Status: "pending", ActiveForm: "Lasting"},
		}, got[0].Todos)
		assert.Equal(t, 2, got[0].Open())
	})

	t.Run("task layout wins over legacy for the same session", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoSessionA+".json",
			`[{"content":"legacy item","status":"pending"}]`)
		writeTaskFile(t, tmpDir, todoSessionA, "1.json", `{"id":"1","subject":"task item","status":"pending"}`)

		got, err := ListTodos(tmpDir)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "task item", got[0].Todos[0].Content)
	})

	t.Run("skips non-uuid and empty task dirs", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(TasksDir(tmpDir), "not-a-uuid"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(TasksDir(tmpDir), todoSessionA), 0o755))
		writeTaskFile(t, tmpDir, todoSessionB, "1.json", `not json`)

		got, err := ListTodos(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("unreadable tasks dir errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(TasksDir(tmpDir), []byte("x"), 0o644))
		_, err := ListTodos(tmpDir)
		require.Error(t, err)
	})
}

func TestDeleteTodos(t *testing.T) {
	t.Run("removes main and subagent files for the session only", func(t *testing.T) {
		tmpDir := t.TempDir()
		mainFile := writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoSessionA+".json", `[]`)
		subFile := writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoAgentX+".json", `[]`)
		otherFile := writeTodoFile(t, tmpDir, todoSessionB+"-agent-"+todoSessionB+".json", `[]`)

		require.NoError(t, DeleteTodos(tmpDir, todoSessionA))
		assert.NoFileExists(t, mainFile)
		assert.NoFileExists(t, subFile)
		assert.FileExists(t, otherFile)
	})

	t.Run("removes the session's task dir only", func(t *testing.T) {
		tmpDir := t.TempDir()
		doomed := writeTaskFile(t, tmpDir, todoSessionA, "1.json", `{"subject":"x","status":"pending"}`)
		kept := writeTaskFile(t, tmpDir, todoSessionB, "1.json", `{"subject":"y","status":"pending"}`)

		require.NoError(t, DeleteTodos(tmpDir, todoSessionA))
		assert.NoDirExists(t, filepath.Dir(doomed))
		assert.FileExists(t, kept)
	})

	t.Run("missing todos dir is not an error", func(t *testing.T) {
		require.NoError(t, DeleteTodos(t.TempDir(), todoSessionA))
	})

	t.Run("empty session id deletes nothing", func(t *testing.T) {
		tmpDir := t.TempDir()
		stray := writeTodoFile(t, tmpDir, "settings.json", `{}`)
		require.NoError(t, DeleteTodos(tmpDir, ""))
		assert.FileExists(t, stray)
	})

	t.Run("unreadable todos dir errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(TodosDir(tmpDir), []byte("x"), 0o644))
		require.Error(t, DeleteTodos(tmpDir, todoSessionA))
	})
}

func TestDelete_RemovesTodos(t *testing.T) {
	tmpDir := t.TempDir()
	workDir := filepath.Join(tmpDir, "-work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	transcript := filepath.Join(workDir, todoSessionA+".jsonl")
	require.NoError(t, os.WriteFile(transcript, []byte("{}"), 0o644))
	todoFile := writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoSessionA+".json", `[]`)

	require.NoError(t, Delete(tmpDir, Session{ID: todoSessionA, Subdir: "-work"}))
	assert.NoFileExists(t, transcript)
	assert.NoFileExists(t, todoFile)
}

func TestOrphanedTodos(t *testing.T) {
	t.Run("todo files without transcript are orphans, grouped by session", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoSessionA+".json", `[]`)
		writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoAgentX+".json", `[]`)

		got, err := OrphanedTodos(tmpDir)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, todoSessionA, got[0].SessionID)
		assert.False(t, got[0].ModTime.IsZero(), "ModTime should come from the todo files")
	})

	t.Run("transcript in subdir keeps the todos", func(t *testing.T) {
		tmpDir := t.TempDir()
		workDir := filepath.Join(tmpDir, "-work")
		require.NoError(t, os.MkdirAll(workDir, 0o755))
		// The transcript need not be parseable; its presence keeps the todos.
		require.NoError(t, os.WriteFile(filepath.Join(workDir, todoSessionB+".jsonl"), []byte("not json"), 0o644))
		writeTodoFile(t, tmpDir, todoSessionB+"-agent-"+todoSessionB+".json", `[]`)
		writeTodoFile(t, tmpDir, todoSessionA+"-agent-"+todoSessionA+".json", `[]`)

		got, err := OrphanedTodos(tmpDir)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, todoSessionA, got[0].SessionID)
	})

	t.Run("non-todo files ignored", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeTodoFile(t, tmpDir, "settings.json", `{}`)

		got, err := OrphanedTodos(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("task dir without transcript is an orphan", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeTaskFile(t, tmpDir, todoSessionA, "1.json", `{"subject":"x","status":"pending"}`)
		require.NoError(t, os.MkdirAll(filepath.Join(TasksDir(tmpDir), "not-a-uuid"), 0o755))

		got, err := OrphanedTodos(tmpDir)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, todoSessionA, got[0].SessionID)
		assert.False(t, got[0].ModTime.IsZero(), "ModTime should come from the task files")
	})

	t.Run("task dir with transcript is kept", func(t *testing.T) {
		tmpDir := t.TempDir()
		workDir := filepath.Join(tmpDir, "-work")
		require.NoError(t, os.MkdirAll(workDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(workDir, todoSessionB+".jsonl"), []byte("{}"), 0o644))
		writeTaskFile(t, tmpDir, todoSessionB, "1.json", `{"subject":"x","status":"pending"}`)

		got, err := OrphanedTodos(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("missing todos and tasks dirs yield none", func(t *testing.T) {
		got, err := OrphanedTodos(t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("unreadable todos dir errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(TodosDir(tmpDir), []byte("x"), 0o644))
		_, err := OrphanedTodos(tmpDir)
		require.Error(t, err)
	})

	t.Run("unreadable tasks dir errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(TasksDir(tmpDir), []byte("x"), 0o644))
		_, err := OrphanedTodos(tmpDir)
		require.Error(t, err)
	})
}

func TestProjectDirs(t *testing.T) {
	t.Run("returns only project dirs", func(t *testing.T) {
		tmpDir := t.TempDir()
		for _, d := range []string{"-work", "-home-user-blog", "plugins", "caches"} {
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, d), 0o755))
		}
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "-stray-file"), []byte("x"), 0o644))

		got, err := ProjectDirs(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, []string{"-home-user-blog", "-work"}, got)
	})

	t.Run("missing forge dir yields none", func(t *testing.T) {
		got, err := ProjectDirs(filepath.Join(t.TempDir(), "missing"))
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("unreadable forge dir errors", func(t *testing.T) {
		notADir := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o644))
		_, err := ProjectDirs(notADir)
		require.Error(t, err)
	})
}
