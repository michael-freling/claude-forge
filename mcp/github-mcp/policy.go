package main

import (
	"fmt"
	"strings"
)

// Policy enforces owner/repo restrictions on tool calls.
type Policy struct {
	AllowedOwner string
	AllowedRepo  string
}

// CheckTool validates whether a tool call is allowed based on the tool's
// read/write classification and the target owner/repo.
//
// Rules:
//   - Read operations: always allowed (any repo)
//   - Write operations: only allowed if targetOwner/targetRepo matches
//     AllowedOwner/AllowedRepo (case-insensitive)
func (p *Policy) CheckTool(toolName string, isWrite bool, targetOwner, targetRepo string) error {
	if !isWrite {
		return nil
	}

	if !strings.EqualFold(targetOwner, p.AllowedOwner) || !strings.EqualFold(targetRepo, p.AllowedRepo) {
		return fmt.Errorf("write access denied: tool %s targets %s/%s but only %s/%s is allowed",
			toolName, targetOwner, targetRepo, p.AllowedOwner, p.AllowedRepo)
	}

	return nil
}
