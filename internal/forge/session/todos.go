package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// todosDirName and tasksDirName are the per-project subdirectories holding
// Claude Code's TodoWrite state. They live inside the project session dir so
// transcripts and todo lists are pruned together, and are bind-mounted to
// ~/.claude/todos and ~/.claude/tasks in the agent so lists survive container
// cleanup and reappear on resume. Older Claude Code versions write
// todos/<session-id>-agent-<agent-id>.json (one JSON array per session);
// newer versions write tasks/<session-id>/<n>.json (one file per item).
const (
	todosDirName = "todos"
	tasksDirName = "tasks"
)

// TodosDir returns the legacy-layout todos directory for a project session dir.
func TodosDir(sessionDir string) string {
	return filepath.Join(sessionDir, todosDirName)
}

// TasksDir returns the task-layout todos directory for a project session dir.
func TasksDir(sessionDir string) string {
	return filepath.Join(sessionDir, tasksDirName)
}

// Todo is a single TodoWrite item as Claude Code stores it on disk.
type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"` // "pending", "in_progress", or "completed"
	ActiveForm string `json:"activeForm"`
}

// TodoList is one session's todo state, read from a file under the todos dir.
type TodoList struct {
	SessionID string
	ModTime   time.Time // file mtime: when the list was last updated
	Todos     []Todo
}

// Open returns the number of todos not yet completed.
func (l TodoList) Open() int {
	n := 0
	for _, t := range l.Todos {
		if t.Status != "completed" {
			n++
		}
	}
	return n
}

// todoFileSessionID extracts the owning session UUID from a todo filename and
// reports whether the file is the session's main-thread list. Claude Code
// names todo files <session-id>-agent-<agent-id>.json, where the agent id
// equals the session id for the main thread and differs for subagents; a bare
// <session-id>.json (older versions) is also a main-thread list. Files that
// match neither shape yield "".
func todoFileSessionID(name string) (id string, main bool) {
	base, ok := strings.CutSuffix(name, ".json")
	if !ok {
		return "", false
	}
	if sid, aid, found := strings.Cut(base, "-agent-"); found {
		if !sidecarIDPattern.MatchString(sid) {
			return "", false
		}
		return sid, aid == sid
	}
	if !sidecarIDPattern.MatchString(base) {
		return "", false
	}
	return base, true
}

// ListTodos reads the main-thread todo lists of a project's sessions from both
// on-disk layouts, sorted by last update (most recent first). Subagent lists,
// unparseable files, and empty lists are skipped; missing dirs yield none.
// A session present in both layouts keeps its task-layout (newer) list.
func ListTodos(sessionDir string) ([]TodoList, error) {
	legacy, err := listLegacyTodos(sessionDir)
	if err != nil {
		return nil, err
	}
	tasks, err := listTaskTodos(sessionDir)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]TodoList, len(legacy)+len(tasks))
	for _, l := range legacy {
		byID[l.SessionID] = l
	}
	for _, l := range tasks {
		byID[l.SessionID] = l
	}
	lists := make([]TodoList, 0, len(byID))
	for _, l := range byID {
		lists = append(lists, l)
	}
	sort.Slice(lists, func(i, j int) bool { return lists[i].ModTime.After(lists[j].ModTime) })
	return lists, nil
}

// listLegacyTodos reads the todos/<session-id>-agent-<agent-id>.json layout.
func listLegacyTodos(sessionDir string) ([]TodoList, error) {
	dir := TodosDir(sessionDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read todos directory: %w", err)
	}

	var lists []TodoList
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		sid, main := todoFileSessionID(e.Name())
		if sid == "" || !main {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var todos []Todo
		if err := json.Unmarshal(data, &todos); err != nil || len(todos) == 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // vanished between ReadDir and Info
		}
		lists = append(lists, TodoList{SessionID: sid, ModTime: info.ModTime(), Todos: todos})
	}
	return lists, nil
}

// taskFile is one item in the tasks/<session-id>/<n>.json layout; subject maps
// onto Todo.Content.
type taskFile struct {
	Subject    string `json:"subject"`
	ActiveForm string `json:"activeForm"`
	Status     string `json:"status"`
}

// listTaskTodos reads the tasks/<session-id>/<n>.json layout.
func listTaskTodos(sessionDir string) ([]TodoList, error) {
	dir := TasksDir(sessionDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read tasks directory: %w", err)
	}

	var lists []TodoList
	for _, e := range entries {
		if !e.IsDir() || !sidecarIDPattern.MatchString(e.Name()) {
			continue
		}
		list, ok := readTaskDir(filepath.Join(dir, e.Name()))
		if !ok {
			continue
		}
		list.SessionID = e.Name()
		lists = append(lists, list)
	}
	return lists, nil
}

// readTaskDir assembles one session's todo list from its task directory,
// ordered by the numeric file name. Reports false when the directory holds no
// parseable items.
func readTaskDir(dir string) (TodoList, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return TodoList{}, false
	}
	type numbered struct {
		n    int
		todo Todo
	}
	var items []numbered
	var newest time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(base)
		if err != nil {
			continue // e.g. the .lock file
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var tf taskFile
		if err := json.Unmarshal(data, &tf); err != nil || tf.Subject == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		items = append(items, numbered{n: n, todo: Todo{Content: tf.Subject, Status: tf.Status, ActiveForm: tf.ActiveForm}})
	}
	if len(items) == 0 {
		return TodoList{}, false
	}
	sort.Slice(items, func(i, j int) bool { return items[i].n < items[j].n })
	todos := make([]Todo, len(items))
	for i, it := range items {
		todos[i] = it.todo
	}
	return TodoList{ModTime: newest, Todos: todos}, true
}

// DeleteTodos removes every todo file belonging to a session in both layouts:
// legacy todo files (main-thread and subagent) and the session's task
// directory. Missing dirs or files are not an error.
func DeleteTodos(sessionDir, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	dir := TodosDir(sessionDir)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read todos directory: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if sid, _ := todoFileSessionID(e.Name()); sid != sessionID {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove todo file: %w", err)
		}
	}
	if err := os.RemoveAll(filepath.Join(TasksDir(sessionDir), sessionID)); err != nil {
		return fmt.Errorf("failed to remove task directory: %w", err)
	}
	return nil
}

// OrphanedTodo identifies a session that still owns todo files but whose
// transcript no longer exists, with the most recent todo update time so
// callers can apply an age policy without re-statting the files.
type OrphanedTodo struct {
	SessionID string
	ModTime   time.Time
}

// OrphanedTodos returns, sorted by session ID, the sessions owning todo state
// in either layout (legacy todo files or a task directory) whose transcript
// ("<id>.jsonl") no longer exists, neither at the top level nor in any
// first-level subdirectory. The transcript's mere presence keeps the todos; it
// need not be parseable. Missing todos/tasks dirs yield no orphans.
func OrphanedTodos(sessionDir string) ([]OrphanedTodo, error) {
	todoEntries, err := os.ReadDir(TodosDir(sessionDir))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read todos directory: %w", err)
	}
	taskEntries, err := os.ReadDir(TasksDir(sessionDir))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read tasks directory: %w", err)
	}
	if len(todoEntries) == 0 && len(taskEntries) == 0 {
		return nil, nil
	}

	hasTranscript := transcriptIDs(sessionDir)
	newest := make(map[string]time.Time)
	record := func(sid string, mt time.Time) {
		if mt.After(newest[sid]) {
			newest[sid] = mt
		}
	}

	for _, e := range todoEntries {
		if e.IsDir() {
			continue
		}
		sid, _ := todoFileSessionID(e.Name())
		if sid == "" || hasTranscript[sid] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // vanished between ReadDir and Info
		}
		record(sid, info.ModTime())
	}

	for _, e := range taskEntries {
		if !e.IsDir() || !sidecarIDPattern.MatchString(e.Name()) || hasTranscript[e.Name()] {
			continue
		}
		record(e.Name(), taskDirMtime(filepath.Join(TasksDir(sessionDir), e.Name()), e))
	}

	var orphans []OrphanedTodo
	for sid, mt := range newest {
		orphans = append(orphans, OrphanedTodo{SessionID: sid, ModTime: mt})
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].SessionID < orphans[j].SessionID })
	return orphans, nil
}

// taskDirMtime returns a task directory's last activity: the newest mtime
// among its files, or the directory's own mtime when it has none.
func taskDirMtime(dir string, entry os.DirEntry) time.Time {
	var newest time.Time
	if info, err := entry.Info(); err == nil {
		newest = info.ModTime()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return newest
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

// ProjectDirs returns, sorted, the per-project session directory names under
// the forge data dir (~/.claude-forge). Project IDs are host directory paths
// with "/" replaced by "-", so they always begin with "-"; every other entry
// (plugins, caches) is skipped. A missing forgeDir yields none.
func ProjectDirs(forgeDir string) ([]string, error) {
	entries, err := os.ReadDir(forgeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read forge data directory: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "-") {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}
