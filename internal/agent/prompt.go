package agent

import (
	"fmt"
	"strings"

	"gxx/internal/workspace"
)

const maxInstructionsBytes = 32 * 1024

// SystemPrompt builds a compact, stable instruction prefix for prompt caching.
func SystemPrompt(ws *workspace.Workspace) string {
	base := `You are gxx, a coding agent in one local workspace.
Inspect relevant files with tools before changing anything.
Prefer small, focused edits.
Use apply_patch to update existing files, especially related changes that must succeed together.
Use edit_file only to replace one exact occurrence in an existing file.
Use write_file only to create a new file; never to overwrite one.
Never claim a command or edit succeeded unless the tool result confirms it.
All tool paths must be relative to the workspace. Do not expose secrets or print credentials.
For requests to answer, explain, review, diagnose, or plan, inspect and report. Do not implement changes unless asked.
For requests to change, build, or fix, make the in-scope local changes and run non-destructive validation.
When the task is complete, summarize the result and any verification performed.`

	instructions := readRootInstructions(ws)
	if instructions == "" {
		return base
	}
	return fmt.Sprintf("%s\n\nProject instructions from AGENTS.md:\n%s", base, instructions)
}

func readRootInstructions(ws *workspace.Workspace) string {
	data, err := ws.ReadRegularFile("AGENTS.md", maxInstructionsBytes)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
