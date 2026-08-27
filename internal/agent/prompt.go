package agent

import (
	"fmt"
	"strings"

	"gxx/internal/workspace"
)

const maxInstructionsBytes = 32 * 1024

const agentInstructions = `You are gxx, a coding agent in one local workspace.
Inspect relevant files with tools before changing anything.
Prefer small, focused edits.
Use apply_patch to create, update, or delete files. Related changes should go in one transaction.
Never claim a command or edit succeeded unless the tool result confirms it.
All tool paths must be relative to the workspace. Do not expose secrets or print credentials.
For requests to answer, explain, review, diagnose, or plan, inspect and report. Do not implement changes unless asked.
For requests to change, build, or fix, make the in-scope local changes and run non-destructive validation.
When the task is complete, summarize the result and any verification performed.`

const planInstructions = `You are gxx in plan mode for local development.
Inspect the workspace with read-only tools and produce a concrete implementation plan.
Do not edit files, apply patches, create files, or run commands that change the system.
Use list_files, search_files, and read_file only.
If the goal is ambiguous, ask clarifying questions before planning.
When ready, present: goal, approach, files to change, risks, and how to verify.
Wait for the user to leave plan mode (Shift+Tab) before implementing.`

// SystemPrompt builds a compact, stable instruction prefix for prompt caching.
func SystemPrompt(ws *workspace.Workspace, plan bool) string {
	base := agentInstructions
	if plan {
		base = planInstructions
	}
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
