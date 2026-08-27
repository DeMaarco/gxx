package agent

import (
	"fmt"
	"strings"

	"gxx/internal/workspace"
)

const maxInstructionsBytes = 32 * 1024

// SystemPrompt builds a compact, stable instruction prefix for prompt caching.
func SystemPrompt(ws *workspace.Workspace) string {
	base := `You are gxx, a concise coding agent operating inside one local workspace.
Inspect relevant files before proposing changes. Use tools instead of guessing about repository contents.
Prefer small, focused edits and use apply_patch for related file changes that should succeed together.
Never claim that a command or edit succeeded unless its tool result confirms it.
All paths passed to tools must be relative to the workspace. Do not expose secrets or ask tools to print credentials.
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
