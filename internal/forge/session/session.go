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

// GenerateUUID returns a random RFC 4122 version 4 UUID, suitable for use as
// Claude Code's --session-id. Pinning the session ID lets us name the sidecar
// metadata file (and the resulting JSONL) by a known identifier.
func GenerateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate session UUID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// Session represents a claude-forge session.
type Session struct {
	ID        string
	CreatedAt time.Time
	FirstMsg  string
	Name      string // human-readable name from the sidecar metadata file
	Subdir    string // relative subdirectory within session dir (e.g., "-work", "-work-.claude-worktrees-feature")
}

// Metadata is sidecar information about a session, stored next to the JSONL
// file as <session-id>.json under the project's session directory.
type Metadata struct {
	Name string `json:"name"`
}

// metadataPath returns the sidecar metadata path for a session ID.
func metadataPath(sessionDir, sessionID string) string {
	return filepath.Join(sessionDir, sessionID+".json")
}

// ValidateName checks that a session name is safe to store and display. Names
// must not contain tab or newline characters, which would corrupt the
// tab-separated `resume --list` output.
func ValidateName(name string) error {
	if strings.ContainsAny(name, "\t\n\r") {
		return fmt.Errorf("session name must not contain tab or newline characters")
	}
	return nil
}

// WriteMetadata writes the sidecar metadata file for a session.
func WriteMetadata(sessionDir, sessionID string, meta Metadata) error {
	if err := ValidateName(meta.Name); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session metadata: %w", err)
	}
	if err := os.WriteFile(metadataPath(sessionDir, sessionID), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write session metadata: %w", err)
	}
	return nil
}

// readMetadata reads the sidecar metadata for a session. A missing or invalid
// file yields a zero Metadata and no error.
func readMetadata(sessionDir, sessionID string) Metadata {
	data, err := os.ReadFile(metadataPath(sessionDir, sessionID))
	if err != nil {
		return Metadata{}
	}
	var meta Metadata
	_ = json.Unmarshal(data, &meta)
	return meta
}

const worktreeSubdirPrefix = "-work--claude-worktrees-"

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
// Claude Code uses "user"/"assistant" types with message as a nested object
// containing {role, content}. Older/test formats may use "human"/"system"
// with message as a plain string.
type jsonLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

// extractContent returns the text content from a message field,
// handling both string values and {"role":"...","content":"..."} objects.
func extractContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Content
	}
	return ""
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
			sess.Name = readMetadata(sessionDir, sessionID).Name
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

		// Extract first user message (Claude Code uses "user"; older format uses "human")
		if (entry.Type == "user" || entry.Type == "human") && firstMsg == "" {
			firstMsg = extractContent(entry.Message)
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
