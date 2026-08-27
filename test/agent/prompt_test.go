package agent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gxx/internal/agent"

	"gxx/internal/workspace"
)

func TestSystemPromptReadsOnlyBoundedWorkspaceInstructions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Use focused tests."), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	prompt := agent.SystemPrompt(ws, false)
	if !strings.Contains(prompt, "Use focused tests.") {
		t.Fatalf("prompt = %q, want project instructions", prompt)
	}
	if !strings.Contains(prompt, "make the in-scope local changes") {
		t.Fatalf("prompt = %q, want agent instructions", prompt)
	}

	if err := os.WriteFile(
		filepath.Join(root, "AGENTS.md"),
		[]byte(strings.Repeat("x", agent.MaxInstructionsBytes+1)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	prompt = agent.SystemPrompt(ws, false)
	if strings.Contains(prompt, "Project instructions") {
		t.Fatalf("oversized AGENTS.md was included")
	}
}

func TestSystemPromptRejectsOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "instructions.txt")
	if err := os.WriteFile(outside, []byte("EXTERNAL SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	prompt := agent.SystemPrompt(ws, false)
	if strings.Contains(prompt, "EXTERNAL SECRET") {
		t.Fatalf("outside symlink content leaked into prompt")
	}
}

func TestSystemPromptPlanModeUsesReadOnlyInstructions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Keep tests green."), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	prompt := agent.SystemPrompt(ws, true)
	if !strings.Contains(prompt, "plan mode") {
		t.Fatalf("prompt = %q, want plan mode instructions", prompt)
	}
	if strings.Contains(prompt, "make the in-scope local changes") {
		t.Fatalf("plan prompt included agent implementation instructions")
	}
	if !strings.Contains(prompt, "Keep tests green.") {
		t.Fatalf("prompt = %q, want project instructions", prompt)
	}
}
