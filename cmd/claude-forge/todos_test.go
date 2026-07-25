package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michael-freling/claude-forge/internal/forge/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	todoSessA = "11111111-2222-4333-8444-555555555555"
	todoSessB = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

// seedTodoProject creates a project dir under forgeDir with a transcript,
// a name sidecar, and a todo list for one session, returning the todo path.
func seedTodoProject(t *testing.T, forgeDir, projectID, sessionID, name, todosJSON string) string {
	t.Helper()
	sessionDir := filepath.Join(forgeDir, projectID)
	workDir := filepath.Join(sessionDir, "-work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	recent := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	writeSessionFile(t, filepath.Join(workDir, sessionID+".jsonl"), recent, "first message of "+sessionID)
	if name != "" {
		require.NoError(t, session.WriteMetadata(sessionDir, sessionID, session.Metadata{Name: name}))
	}
	todosDir := session.TodosDir(sessionDir)
	require.NoError(t, os.MkdirAll(todosDir, 0o755))
	todoPath := filepath.Join(todosDir, sessionID+"-agent-"+sessionID+".json")
	require.NoError(t, os.WriteFile(todoPath, []byte(todosJSON), 0o644))
	return todoPath
}

func TestListTodos_Output(t *testing.T) {
	forgeDir := t.TempDir()
	older := seedTodoProject(t, forgeDir, "-home-user-blog", todoSessB, "",
		`[{"content":"draft outline","status":"pending"}]`)
	ageFile(t, older, 2*time.Hour)
	seedTodoProject(t, forgeDir, "-work", todoSessA, "fix-auth",
		`[{"content":"wire retry logic","status":"in_progress"},{"content":"fix login redirect","status":"completed"}]`)

	var buf bytes.Buffer
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-home-user-blog", "-work"}, false))
	out := buf.String()

	// Grouped by project, most recently active project first.
	workIdx := strings.Index(out, "-work\n")
	blogIdx := strings.Index(out, "-home-user-blog\n")
	require.GreaterOrEqual(t, workIdx, 0, "work project header missing: %s", out)
	require.GreaterOrEqual(t, blogIdx, 0, "blog project header missing: %s", out)
	assert.Less(t, workIdx, blogIdx, "most recently active project should come first")

	// The session label sits under its project heading.
	assert.Contains(t, out, "fix-auth · updated")
	assert.Contains(t, out, "[~] wire retry logic")
	assert.Contains(t, out, "[x] fix login redirect")
	assert.Contains(t, out, "[ ] draft outline")
	// Unnamed session falls back to its opening message.
	assert.Contains(t, out, `"first message of `+todoSessB+`"`)
}

func TestListTodos_HidesCompletedUnlessAll(t *testing.T) {
	forgeDir := t.TempDir()
	seedTodoProject(t, forgeDir, "-work", todoSessA, "done-work",
		`[{"content":"shipped","status":"completed"}]`)

	var buf bytes.Buffer
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-work"}, false))
	assert.Contains(t, buf.String(), "No open TODOs found. Use --all to include completed ones.")

	buf.Reset()
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-work"}, true))
	assert.Contains(t, buf.String(), "done-work")
	assert.Contains(t, buf.String(), "[x] shipped")
}

func TestListTodos_NoProjects(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, listTodos(&buf, t.TempDir(), nil, true))
	assert.Contains(t, buf.String(), "No TODOs found.")
}

func TestListTodos_ListWithoutTranscript(t *testing.T) {
	forgeDir := t.TempDir()
	sessionDir := filepath.Join(forgeDir, "-work")
	todosDir := session.TodosDir(sessionDir)
	require.NoError(t, os.MkdirAll(todosDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(todosDir, todoSessA+"-agent-"+todoSessA+".json"),
		[]byte(`[{"content":"orphan work","status":"pending"}]`), 0o644))

	var buf bytes.Buffer
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-work"}, false))
	// No transcript metadata: the session label falls back to the session ID.
	assert.Contains(t, buf.String(), todoSessA+" · updated")
	assert.Contains(t, buf.String(), "[ ] orphan work")
}

func TestListTodos_TaskLayout(t *testing.T) {
	forgeDir := t.TempDir()
	sessionDir := filepath.Join(forgeDir, "-work")
	workDir := filepath.Join(sessionDir, "-work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	recent := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	writeSessionFile(t, filepath.Join(workDir, todoSessA+".jsonl"), recent, "task layout session")
	taskDir := filepath.Join(session.TasksDir(sessionDir), todoSessA)
	require.NoError(t, os.MkdirAll(taskDir, 0o755))
	// Shape captured from a real agent-image run (Claude Code task layout).
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "1.json"),
		[]byte(`{"id":"1","subject":"verify todos mount","description":"","activeForm":"Verifying","status":"in_progress","blocks":[],"blockedBy":[]}`), 0o644))

	var buf bytes.Buffer
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-work"}, false))
	assert.Contains(t, buf.String(), `"task layout session"`)
	assert.Contains(t, buf.String(), "[~] verify todos mount")
}

func TestListTodos_ShowsBacklog(t *testing.T) {
	forgeDir := t.TempDir()
	sessionDir := filepath.Join(forgeDir, "-work")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	require.NoError(t, session.AddBacklogItem(sessionDir, "fix flaky auth test"))
	require.NoError(t, session.AddBacklogItem(sessionDir, "write migration"))
	require.NoError(t, session.SetBacklogItemDone(sessionDir, 2, true))

	var buf bytes.Buffer
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-work"}, false))
	out := buf.String()

	assert.Contains(t, out, "-work\n")
	assert.Contains(t, out, "backlog · updated")
	// Numbered in file order so `todos done <n>` lines up, done item marked.
	assert.Contains(t, out, "[ ] 1. fix flaky auth test")
	assert.Contains(t, out, "[x] 2. write migration")
}

func TestListTodos_ShowsSessionTag(t *testing.T) {
	forgeDir := t.TempDir()
	sessionDir := filepath.Join(forgeDir, "-work")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	// An item the agent tagged with its session, and one added from the host.
	require.NoError(t, os.WriteFile(session.BacklogPath(sessionDir), []byte(
		"- [ ] wire retries (@fix-auth)\n- [ ] plain host item\n"), 0o644))

	var buf bytes.Buffer
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-work"}, false))
	out := buf.String()
	assert.Contains(t, out, "[ ] 1. wire retries (@fix-auth)")
	assert.Contains(t, out, "[ ] 2. plain host item")
}

func TestListTodos_BacklogAllDoneHiddenUnlessAll(t *testing.T) {
	forgeDir := t.TempDir()
	sessionDir := filepath.Join(forgeDir, "-work")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	require.NoError(t, session.AddBacklogItem(sessionDir, "already shipped"))
	require.NoError(t, session.SetBacklogItemDone(sessionDir, 1, true))

	var buf bytes.Buffer
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-work"}, false))
	assert.Contains(t, buf.String(), "No open TODOs found")

	buf.Reset()
	require.NoError(t, listTodos(&buf, forgeDir, []string{"-work"}, true))
	assert.Contains(t, buf.String(), "[x] 1. already shipped")
}

func TestBacklogHasOpen(t *testing.T) {
	assert.False(t, backlogHasOpen(nil))
	assert.False(t, backlogHasOpen([]session.BacklogItem{{Text: "a", Done: true}}))
	assert.True(t, backlogHasOpen([]session.BacklogItem{{Text: "a", Done: true}, {Text: "b"}}))
}

func TestBacklogMarker(t *testing.T) {
	assert.Equal(t, "[x]", backlogMarker(true))
	assert.Equal(t, "[ ]", backlogMarker(false))
}

// chdirToRepo sets HOME to a temp dir, chdirs into a fresh git repo, and
// returns the project's session dir under ~/.claude-forge.
func chdirToRepo(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoDir := setupTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })
	return filepath.Join(homeDir, ".claude-forge", strings.ReplaceAll(repoDir, "/", "-"))
}

func TestTodosAddCmd(t *testing.T) {
	sessionDir := chdirToRepo(t)

	cmd := newTodosAddCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"fix", "the", "flaky", "test"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, buf.String(), "Added: fix the flaky test")
	items, err := session.ReadBacklog(sessionDir)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "fix the flaky test", items[0].Text)
	assert.False(t, items[0].Done)
}

func TestTodosDoneCmd(t *testing.T) {
	sessionDir := chdirToRepo(t)
	require.NoError(t, session.AddBacklogItem(sessionDir, "one"))
	require.NoError(t, session.AddBacklogItem(sessionDir, "two"))

	done := newTodosDoneCmd()
	var buf bytes.Buffer
	done.SetOut(&buf)
	done.SetArgs([]string{"2"})
	require.NoError(t, done.Execute())
	assert.Contains(t, buf.String(), "Marked done: item 2")

	items, err := session.ReadBacklog(sessionDir)
	require.NoError(t, err)
	assert.False(t, items[0].Done)
	assert.True(t, items[1].Done)

	// --reopen unmarks it.
	reopen := newTodosDoneCmd()
	buf.Reset()
	reopen.SetOut(&buf)
	reopen.SetArgs([]string{"2", "--reopen"})
	require.NoError(t, reopen.Execute())
	assert.Contains(t, buf.String(), "Reopened: item 2")
	items, err = session.ReadBacklog(sessionDir)
	require.NoError(t, err)
	assert.False(t, items[1].Done)
}

func TestTodosDoneCmd_Errors(t *testing.T) {
	chdirToRepo(t)

	notANumber := newTodosDoneCmd()
	notANumber.SetArgs([]string{"abc"})
	require.Error(t, notANumber.Execute())

	outOfRange := newTodosDoneCmd()
	outOfRange.SetArgs([]string{"9"})
	require.Error(t, outOfRange.Execute())
}

func TestTodosEditCmd(t *testing.T) {
	sessionDir := chdirToRepo(t)

	var editedPath string
	orig := backlogEditor
	backlogEditor = func(path string) error {
		editedPath = path
		return os.WriteFile(path, []byte("- [ ] added in editor\n"), 0o644)
	}
	t.Cleanup(func() { backlogEditor = orig })

	cmd := newTodosEditCmd()
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, session.BacklogPath(sessionDir), editedPath)
	items, err := session.ReadBacklog(sessionDir)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "added in editor", items[0].Text)
}

func TestTodosCmd_ShowsBacklogForCurrentProject(t *testing.T) {
	sessionDir := chdirToRepo(t)
	require.NoError(t, session.AddBacklogItem(sessionDir, "curate this"))

	cmd := newTodosCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--project"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, buf.String(), "backlog · updated")
	assert.Contains(t, buf.String(), "[ ] 1. curate this")
}

func TestTodoEntry_SessionLabel(t *testing.T) {
	long := strings.Repeat("m", 80)
	tests := []struct {
		name  string
		entry todoEntry
		want  string
	}{
		{"name wins", todoEntry{name: "fix-auth", firstMsg: "hello"}, "fix-auth"},
		{"first message fallback", todoEntry{firstMsg: " hello "}, `"hello"`},
		{"long first message truncated", todoEntry{firstMsg: long}, `"` + long[:57] + `..."`},
		{"session id fallback", todoEntry{list: session.TodoList{SessionID: todoSessA}}, todoSessA},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.entry.sessionLabel())
		})
	}
}

func TestTodoMarker(t *testing.T) {
	assert.Equal(t, "[x]", todoMarker("completed"))
	assert.Equal(t, "[~]", todoMarker("in_progress"))
	assert.Equal(t, "[ ]", todoMarker("pending"))
	assert.Equal(t, "[ ]", todoMarker(""))
}

func TestFormatAge(t *testing.T) {
	assert.Equal(t, "just now", formatAge(30*time.Second))
	assert.Equal(t, "5m ago", formatAge(5*time.Minute))
	assert.Equal(t, "3h ago", formatAge(3*time.Hour))
	assert.Equal(t, "2d ago", formatAge(49*time.Hour))
}

func TestTodosCmd_AllProjects(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	forgeDir := filepath.Join(homeDir, ".claude-forge")
	seedTodoProject(t, forgeDir, "-work", todoSessA, "fix-auth",
		`[{"content":"wire retry logic","status":"pending"}]`)
	// Non-project entries under the forge dir must not break the walk.
	require.NoError(t, os.MkdirAll(filepath.Join(forgeDir, "plugins"), 0o755))

	cmd := newTodosCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, buf.String(), "-work\n")
	assert.Contains(t, buf.String(), "fix-auth · updated")
	assert.Contains(t, buf.String(), "[ ] wire retry logic")
}

func TestTodosCmd_MissingForgeDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newTodosCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, buf.String(), "No open TODOs found.")
}

func TestTodosCmd_ProjectOnly(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoDir := setupTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	forgeDir := filepath.Join(homeDir, ".claude-forge")
	projectID := strings.ReplaceAll(repoDir, "/", "-")
	seedTodoProject(t, forgeDir, projectID, todoSessA, "this-project",
		`[{"content":"local work","status":"pending"}]`)
	seedTodoProject(t, forgeDir, "-somewhere-else", todoSessB, "other-project",
		`[{"content":"other work","status":"pending"}]`)

	cmd := newTodosCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--project"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, buf.String(), "this-project")
	assert.NotContains(t, buf.String(), "other-project")
}

func TestPruneCmd_Todos(t *testing.T) {
	t.Run("pruned session's todo files are deleted", func(t *testing.T) {
		sessionDir := pruneSetup(t)
		workDir := filepath.Join(sessionDir, "-work")
		seedSession(t, workDir, todoSessA+".jsonl", 31*24*time.Hour)
		todosDir := session.TodosDir(sessionDir)
		require.NoError(t, os.MkdirAll(todosDir, 0o755))
		todoFile := filepath.Join(todosDir, todoSessA+"-agent-"+todoSessA+".json")
		require.NoError(t, os.WriteFile(todoFile, []byte(`[{"content":"x","status":"pending"}]`), 0o644))

		cmd := newPruneCmd()
		cmd.SetArgs([]string{})
		out := captureStdout(t, func() { require.NoError(t, cmd.Execute()) })

		assert.Contains(t, out, "Deleted 1 session")
		assert.NoFileExists(t, todoFile)
	})

	t.Run("orphaned todos swept after grace, kept within it", func(t *testing.T) {
		sessionDir := pruneSetup(t)
		todosDir := session.TodosDir(sessionDir)
		require.NoError(t, os.MkdirAll(todosDir, 0o755))
		oldOrphan := filepath.Join(todosDir, todoSessA+"-agent-"+todoSessA+".json")
		require.NoError(t, os.WriteFile(oldOrphan, []byte(`[]`), 0o644))
		ageFile(t, oldOrphan, 2*time.Hour)
		freshOrphan := filepath.Join(todosDir, todoSessB+"-agent-"+todoSessB+".json")
		require.NoError(t, os.WriteFile(freshOrphan, []byte(`[]`), 0o644))

		cmd := newPruneCmd()
		cmd.SetArgs([]string{})
		out := captureStdout(t, func() { require.NoError(t, cmd.Execute()) })

		assert.Contains(t, out, "deleted orphaned todos  "+todoSessA)
		assert.Contains(t, out, "1 orphaned todo list(s)")
		assert.NoFileExists(t, oldOrphan)
		assert.FileExists(t, freshOrphan, "recently written todos are inside the grace period")
	})

	t.Run("orphaned todos dry-run keeps files", func(t *testing.T) {
		sessionDir := pruneSetup(t)
		todosDir := session.TodosDir(sessionDir)
		require.NoError(t, os.MkdirAll(todosDir, 0o755))
		orphan := filepath.Join(todosDir, todoSessA+"-agent-"+todoSessA+".json")
		require.NoError(t, os.WriteFile(orphan, []byte(`[]`), 0o644))
		ageFile(t, orphan, 2*time.Hour)

		cmd := newPruneCmd()
		cmd.SetArgs([]string{"--dry-run"})
		out := captureStdout(t, func() { require.NoError(t, cmd.Execute()) })

		assert.Contains(t, out, "would delete orphaned todos  "+todoSessA)
		assert.FileExists(t, orphan)
	})
}
