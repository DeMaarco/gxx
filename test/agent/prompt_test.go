// Copyright 2026 DeMarco
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
