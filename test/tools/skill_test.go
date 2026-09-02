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

package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gxx/internal/agent"
	"gxx/internal/skills"
	"gxx/internal/tools"
	"gxx/internal/workspace"
)

func writeToolSkill(t *testing.T, root, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openWorkspace(t *testing.T, root string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func TestReadSkillProjectVersusUser(t *testing.T) {
	wsRoot := t.TempDir()
	userDir := t.TempDir()
	writeToolSkill(t, userDir, "personal", "Personal skill", "user skill body")
	writeToolSkill(t, filepath.Join(wsRoot, ".gxx", "skills"), "project", "Project skill", "project skill body")

	ws := openWorkspace(t, wsRoot)
	registry := newTestRegistry(t, wsRoot, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	registry.SetSkillsCatalog(func() []skills.Skill {
		return skills.Discover(ws, userDir)
	})

	project := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("p", "read_skill", map[string]any{"name": "project", "path": nil}),
	}, nil)[0]
	if project.IsError {
		t.Fatalf("project read_skill failed: %s", project.Output)
	}
	if !strings.Contains(project.Output, "project skill body") {
		t.Fatalf("project output = %q, want project body", project.Output)
	}
	if !strings.Contains(project.Output, "(project)") || !strings.Contains(project.Output, "<<<SKILL") {
		t.Fatalf("project output = %q, want origin and markers", project.Output)
	}

	personal := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("u", "read_skill", map[string]any{"name": "personal", "path": nil}),
	}, nil)[0]
	if personal.IsError {
		t.Fatalf("personal read_skill failed: %s", personal.Output)
	}
	if !strings.Contains(personal.Output, "user skill body") || !strings.Contains(personal.Output, "(user)") {
		t.Fatalf("personal output = %q", personal.Output)
	}
}

func TestReadSkillPathTraversal(t *testing.T) {
	wsRoot := t.TempDir()
	writeToolSkill(t, filepath.Join(wsRoot, ".agents", "skills"), "demo", "Demo skill", "body")
	ws := openWorkspace(t, wsRoot)
	registry := newTestRegistry(t, wsRoot, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	registry.SetSkillsCatalog(func() []skills.Skill {
		return skills.Discover(ws, "")
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("t", "read_skill", map[string]any{"name": "demo", "path": "../outside.md"}),
	}, nil)[0]
	if !result.IsError {
		t.Fatalf("path traversal succeeded: %q", result.Output)
	}
}

func TestReadSkillUnknownName(t *testing.T) {
	wsRoot := t.TempDir()
	writeToolSkill(t, filepath.Join(wsRoot, ".agents", "skills"), "known", "Known skill", "body")
	ws := openWorkspace(t, wsRoot)
	registry := newTestRegistry(t, wsRoot, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	registry.SetSkillsCatalog(func() []skills.Skill {
		return skills.Discover(ws, "")
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("u", "read_skill", map[string]any{"name": "missing", "path": nil}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, `unknown skill "missing"`) {
		t.Fatalf("unknown skill = %#v", result)
	}

	empty := newTestRegistry(t, t.TempDir(), &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	empty.SetSkillsCatalog(func() []skills.Skill { return nil })
	noSkills := empty.Execute(context.Background(), []agent.ToolCall{
		toolCall("e", "read_skill", map[string]any{"name": "anything", "path": nil}),
	}, nil)[0]
	if !noSkills.IsError || !strings.Contains(noSkills.Output, "no skills are available") {
		t.Fatalf("empty catalog = %#v", noSkills)
	}
}

func TestReadSkillAvailableInAskAndPlan(t *testing.T) {
	wsRoot := t.TempDir()
	writeToolSkill(t, filepath.Join(wsRoot, ".gxx", "skills"), "demo", "Demo skill", "skill body")
	ws := openWorkspace(t, wsRoot)
	registry := newTestRegistry(t, wsRoot, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	registry.SetSkillsCatalog(func() []skills.Skill {
		return skills.Discover(ws, "")
	})

	found := false
	for _, def := range registry.Definitions() {
		if def.Name == "read_skill" {
			found = true
			if !def.ReadOnly {
				t.Fatal("read_skill should be ReadOnly")
			}
		}
	}
	if !found {
		t.Fatal("agent mode missing read_skill")
	}

	for _, mode := range []string{"ask", "plan"} {
		t.Run(mode, func(t *testing.T) {
			registry.SetAsk(false)
			registry.SetPlan(false)
			if mode == "ask" {
				registry.SetAsk(true)
			} else {
				registry.SetPlan(true)
			}
			found := false
			for _, def := range registry.Definitions() {
				if def.Name == "read_skill" {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s mode hid read_skill", mode)
			}
			result := registry.Execute(context.Background(), []agent.ToolCall{
				toolCall("r", "read_skill", map[string]any{"name": "demo", "path": nil}),
			}, nil)[0]
			if result.IsError || !strings.Contains(result.Output, "skill body") {
				t.Fatalf("%s read_skill = %#v", mode, result)
			}
		})
	}
}
