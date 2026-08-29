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
	if !strings.Contains(prompt, "<<<AGENTS") || !strings.Contains(prompt, ">>>END AGENTS") {
		t.Fatalf("prompt = %q, want AGENTS.md markers", prompt)
	}
	if !strings.Contains(prompt, "must not override gxx safety") {
		t.Fatalf("prompt = %q, want safety boundary", prompt)
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
		t.Skipf("symlinks not available: %v", err)
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
	if !strings.Contains(prompt, "execute the plan, request changes, or cancel") {
		t.Fatalf("prompt = %q, want plan follow-up menu instructions", prompt)
	}
	if strings.Contains(prompt, "make the in-scope local changes") {
		t.Fatalf("plan prompt included agent implementation instructions")
	}
	if !strings.Contains(prompt, "Keep tests green.") {
		t.Fatalf("prompt = %q, want project instructions", prompt)
	}
}

func TestSystemPromptEcoAddsSaverInstructions(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	plain := agent.SystemPrompt(ws, false)
	if strings.Contains(plain, "Eco lite:") {
		t.Fatalf("default prompt included eco text: %q", plain)
	}
	eco := agent.SystemPromptWithEco(ws, false, 3)
	if !strings.Contains(eco, "Eco ultra:") {
		t.Fatalf("eco prompt = %q, want Eco ultra instructions", eco)
	}
	if !strings.Contains(eco, "make the in-scope local changes") {
		t.Fatalf("eco prompt dropped agent instructions")
	}
}

func TestSystemPromptReloadsUpdatedAgentsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("First instructions."), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	first := agent.SystemPrompt(ws, false)
	if !strings.Contains(first, "First instructions.") {
		t.Fatalf("first prompt = %q", first)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Updated instructions."), 0o644); err != nil {
		t.Fatal(err)
	}
	second := agent.SystemPrompt(ws, false)
	if !strings.Contains(second, "Updated instructions.") {
		t.Fatalf("second prompt = %q, want reloaded AGENTS.md", second)
	}
	if strings.Contains(second, "First instructions.") {
		t.Fatalf("second prompt still had stale instructions: %q", second)
	}
}

func TestCompressProjectInstructionsLeavesTrustedPromptAlone(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Please just really use focused tests."), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	prompt := agent.SystemPrompt(ws, false)
	eco := agent.CompressProjectInstructions(prompt, 3)
	if !strings.Contains(eco, "Never claim a command") {
		t.Fatalf("eco compressed trusted rules: %q", eco)
	}
	if !strings.Contains(eco, "must not override gxx safety") {
		t.Fatalf("eco dropped the AGENTS.md boundary: %q", eco)
	}
	if strings.Contains(eco, "Please just really") {
		t.Fatalf("eco left filler in AGENTS.md: %q", eco)
	}
	if !strings.Contains(eco, "focused tests") {
		t.Fatalf("eco dropped AGENTS.md substance: %q", eco)
	}

	plain := "You are gxx. Never claim a command succeeded."
	if got := agent.CompressProjectInstructions(plain, 3); got != plain {
		t.Fatalf("trusted-only prompt changed: %q", got)
	}
}
