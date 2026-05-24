package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// GenerateID returns 8 random hex characters for use as a session identifier.
func GenerateID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate session ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Session represents a claude-forge session.
type Session struct {
	ID        string
	CreatedAt time.Time
	FirstMsg  string
	Subdir    string // relative subdirectory within session dir (e.g., "-work", "-work-.claude-worktrees-feature")
}

const worktreeSubdirPrefix = "-work-.claude-worktrees-"

// IsWorktree reports whether this session was created inside a Claude Code worktree.
func (s Session) IsWorktree() bool {
	return strings.HasPrefix(s.Subdir, worktreeSubdirPrefix)
}

// WorktreeName returns the worktree name for worktree sessions, or "" otherwise.
func (s Session) WorktreeName() string {
	if !s.IsWorktree() {
		return ""
	}
	return s.Subdir[len(worktreeSubdirPrefix):]
}

// jsonLine represents a single line in a session JSONL file.
type jsonLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

// List reads JSONL session files from the project's session directory.
// sessionDir is the host path like ~/.claude-forge/<project-id>/, which is
// bind-mounted to /home/user/.claude/projects in the container. Claude Code
// stores sessions under <encoded-cwd>/<session-id>.jsonl — typically -work/ for
// the main workspace and -work-.claude-worktrees-<name>/ for each worktree.
//
// To surface all of those in `resume --list`, List walks one level of
// subdirectories. .jsonl files placed directly under sessionDir are also
// included for backward compatibility.
//
// Returns sessions sorted by creation time (most recent first).
func List(sessionDir string) ([]Session, error) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read session directory: %w", err)
	}

	var sessions []Session
	collect := func(dir, subdir string) {
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range dirEntries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
			sess, err := parseSessionFile(sessionID, filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			sess.Subdir = subdir
			sessions = append(sessions, *sess)
		}
	}

	collect(sessionDir, "")
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		collect(filepath.Join(sessionDir, entry.Name()), entry.Name())
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions, nil
}

// parseSessionFile parses a JSONL session file and extracts metadata.
func parseSessionFile(sessionID string, filePath string) (*Session, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	var createdAt time.Time
	var firstMsg string
	foundTimestamp := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry jsonLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Extract timestamp from the first valid entry
		if !foundTimestamp && entry.Timestamp != "" {
			parsed, err := time.Parse(time.RFC3339, entry.Timestamp)
			if err == nil {
				createdAt = parsed
				foundTimestamp = true
			}
		}

		// Extract first user message
		if entry.Type == "human" && firstMsg == "" {
			firstMsg = entry.Message
		}

		// Stop once we have both
		if foundTimestamp && firstMsg != "" {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	if !foundTimestamp {
		return nil, fmt.Errorf("no timestamp found in session file")
	}

	return &Session{
		ID:        sessionID,
		CreatedAt: createdAt,
		FirstMsg:  firstMsg,
	}, nil
}

// Find locates a session by ID across all subdirectories in sessionDir.
func Find(sessionDir, sessionID string) (*Session, error) {
	sessions, err := List(sessionDir)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].ID == sessionID {
			return &sessions[i], nil
		}
	}
	return nil, fmt.Errorf("session %s not found", sessionID)
}
