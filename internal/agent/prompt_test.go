package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	prompt := SystemPrompt(ws)
	if !strings.Contains(prompt, "Use focused tests.") {
		t.Fatalf("prompt = %q, want project instructions", prompt)
	}

	if err := os.WriteFile(
		filepath.Join(root, "AGENTS.md"),
		[]byte(strings.Repeat("x", maxInstructionsBytes+1)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	prompt = SystemPrompt(ws)
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

	prompt := SystemPrompt(ws)
	if strings.Contains(prompt, "EXTERNAL SECRET") {
		t.Fatalf("outside symlink content leaked into prompt")
	}
}
