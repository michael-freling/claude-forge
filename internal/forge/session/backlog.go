package session

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// backlogFileName is the per-project shared backlog: a user-curated Markdown
// checklist living at the root of the project's session dir, bind-mounted into
// every session for that project so both the host CLI and Claude Code can read
// and edit it. Unlike per-session todos, it belongs to the project and is not
// pruned with sessions.
const backlogFileName = "TODO.md"

// BacklogPath returns the shared backlog file path for a project session dir.
func BacklogPath(sessionDir string) string {
	return filepath.Join(sessionDir, backlogFileName)
}

// BacklogItem is a single checklist entry in the shared backlog.
type BacklogItem struct {
	Text    string
	Done    bool
	Session string // the session that added the item, from a trailing "(@name)" marker; empty if none (e.g. added from the host)
}

// backlogItemRe matches a Markdown checklist line, capturing leading
// indentation, the checkbox marker, and the item text. Non-matching lines
// (headings, blank lines, prose) are preserved untouched by in-place edits.
var backlogItemRe = regexp.MustCompile(`^(\s*)- \[([ xX])\]\s?(.*)$`)

// backlogSessionRe matches a trailing "(@name)" session marker on an item.
var backlogSessionRe = regexp.MustCompile(`\s*\(@([^)]*)\)\s*$`)

// backlogHeader documents the shared-backlog convention at the top of a
// freshly created file. It is an HTML comment (ignored by the checklist parser
// and the `todos` view) that tells the in-container agent to tag items it adds
// with its own session name, whose value is in the FORGE_SESSION_NAME env var.
const backlogHeader = "<!-- claude-forge shared backlog. Add tasks as \"- [ ] <task>\". " +
	"When you (the agent) add one, tag it with your session name from the " +
	"FORGE_SESSION_NAME environment variable, e.g. \"- [ ] fix the login bug (@fix-auth)\". -->\n\n"

// ReadBacklog returns the checklist items in a project's shared backlog, in
// file order. Non-checklist lines are ignored; a missing file yields no items.
func ReadBacklog(sessionDir string) ([]BacklogItem, error) {
	f, err := os.Open(BacklogPath(sessionDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open backlog: %w", err)
	}
	defer f.Close()

	var items []BacklogItem
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := backlogItemRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		item := BacklogItem{
			Text: strings.TrimSpace(m[3]),
			Done: m[2] == "x" || m[2] == "X",
		}
		// Split off a trailing "(@name)" session marker into its own field.
		if sm := backlogSessionRe.FindStringSubmatch(item.Text); sm != nil {
			item.Session = strings.TrimSpace(sm[1])
			item.Text = strings.TrimSpace(item.Text[:len(item.Text)-len(sm[0])])
		}
		items = append(items, item)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("failed to read backlog: %w", err)
	}
	return items, nil
}

// EnsureBacklog creates an empty backlog file if none exists, creating the
// project session dir if needed. The orchestrator calls this before starting a
// session so Docker bind-mounts a file (not a freshly created directory) at the
// backlog path; the CLI also calls it so a backlog can be edited for a project
// that has never run a session.
func EnsureBacklog(sessionDir string) error {
	path := BacklogPath(sessionDir)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat backlog: %w", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return fmt.Errorf("failed to create project directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(backlogHeader), 0o644); err != nil {
		return fmt.Errorf("failed to create backlog: %w", err)
	}
	return nil
}

// AddBacklogItem appends a new open item to a project's shared backlog,
// creating the file if needed. The text must be a single non-empty line.
func AddBacklogItem(sessionDir, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("backlog item text is empty")
	}
	if strings.ContainsAny(text, "\r\n") {
		return fmt.Errorf("backlog item must be a single line")
	}

	path := BacklogPath(sessionDir)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read backlog: %w", err)
	}
	if os.IsNotExist(err) {
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			return fmt.Errorf("failed to create project directory: %w", err)
		}
		data = []byte(backlogHeader) // seed the convention header on a brand-new file
	}

	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("- [ ] " + text + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write backlog: %w", err)
	}
	return nil
}

// SetBacklogItemDone flips the checkbox of the n-th checklist item (1-based, in
// file order) while preserving every other line, including the item's own
// indentation and text. It errors if the item does not exist.
func SetBacklogItemDone(sessionDir string, n int, done bool) error {
	if n < 1 {
		return fmt.Errorf("item number must be >= 1")
	}
	path := BacklogPath(sessionDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no backlog items yet")
		}
		return fmt.Errorf("failed to read backlog: %w", err)
	}

	marker := " "
	if done {
		marker = "x"
	}
	lines := strings.Split(string(data), "\n")
	count := 0
	found := false
	for i, line := range lines {
		m := backlogItemRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		count++
		if count == n {
			lines[i] = fmt.Sprintf("%s- [%s] %s", m[1], marker, m[3])
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("backlog item %d not found", n)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return fmt.Errorf("failed to write backlog: %w", err)
	}
	return nil
}
