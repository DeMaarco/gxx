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

package skills_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gxx/internal/skills"
	"gxx/internal/workspace"
)

func writeSkill(t *testing.T, root, name, description, body string) {
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

func writeSkillRaw(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverPrecedenceAndInvalidSkipped(t *testing.T) {
	wsRoot := t.TempDir()
	userDir := t.TempDir()

	writeSkill(t, userDir, "shared", "User shared skill", "user body")
	writeSkill(t, userDir, "personal-only", "Personal only", "personal")
	writeSkill(t, filepath.Join(wsRoot, ".agents", "skills"), "shared", "Agents shared skill", "agents body")
	writeSkill(t, filepath.Join(wsRoot, ".gxx", "skills"), "shared", "Gxx shared skill", "gxx body")
	writeSkill(t, filepath.Join(wsRoot, ".agents", "skills"), "agents-only", "Agents only", "agents")

	bad := filepath.Join(wsRoot, ".agents", "skills", "bad-name")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("---\nname: other\ndescription: nope\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.New(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	catalog := skills.Discover(ws, userDir)
	byName := map[string]skills.Skill{}
	for _, skill := range catalog {
		byName[skill.Name] = skill
	}
	if _, ok := byName["bad-name"]; ok {
		t.Fatal("invalid skill should be skipped")
	}
	if got := byName["shared"]; got.Origin != skills.OriginProject || got.Description != "Gxx shared skill" {
		t.Fatalf("shared = %+v, want gxx project skill", got)
	}
	if got := byName["personal-only"]; got.Origin != skills.OriginUser {
		t.Fatalf("personal-only origin = %q", got.Origin)
	}
	if got := byName["agents-only"]; got.Origin != skills.OriginProject {
		t.Fatalf("agents-only origin = %q", got.Origin)
	}

	body, err := skills.Read(byName["shared"], "")
	if err != nil {
		t.Fatal(err)
	}
	if body != "gxx body" {
		t.Fatalf("Read body = %q, want gxx body", body)
	}
}

func TestDiscoverSkipsInvalidFrontmatter(t *testing.T) {
	userDir := t.TempDir()
	cases := []struct {
		name    string
		content string
	}{
		{"no-front", "just markdown\n"},
		{"unclosed", "---\nname: unclosed\ndescription: x\n"},
		{"empty-desc", "---\nname: empty-desc\ndescription: \n---\n"},
		{"bad-chars", "---\nname: Bad_Chars\ndescription: nope\n---\n"},
		{"double-hyphen", "---\nname: bad--name\ndescription: nope\n---\n"},
		{"leading-hyphen", "---\nname: -leading\ndescription: nope\n---\n"},
		{"missing-name", "---\ndescription: only desc\n---\n"},
	}
	for _, tc := range cases {
		writeSkillRaw(t, userDir, tc.name, tc.content)
	}
	writeSkill(t, userDir, "ok-skill", "Valid skill description", "body")

	catalog := skills.Discover(nil, userDir)
	if len(catalog) != 1 || catalog[0].Name != "ok-skill" {
		t.Fatalf("catalog = %+v, want only ok-skill", catalog)
	}
}

func TestDiscoverRejectsOutsideSymlink(t *testing.T) {
	userDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("---\nname: leak\ndescription: secret skill\n---\nSECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(userDir, "leak")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "SKILL.md")); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}

	catalog := skills.Discover(nil, userDir)
	if len(catalog) != 0 {
		t.Fatalf("catalog = %+v, want empty when SKILL.md is outside symlink", catalog)
	}
}

func TestDiscoverRejectsSymlinkedSkillDirectory(t *testing.T) {
	userDir := t.TempDir()
	realRoot := t.TempDir()
	writeSkill(t, realRoot, "linked", "Linked skill", "body")
	if err := os.Symlink(filepath.Join(realRoot, "linked"), filepath.Join(userDir, "linked")); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}

	catalog := skills.Discover(nil, userDir)
	if len(catalog) != 0 {
		t.Fatalf("catalog = %+v, want empty when skill dir is a symlink", catalog)
	}
}

func TestDiscoverSortsByName(t *testing.T) {
	userDir := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "mid"} {
		writeSkill(t, userDir, name, "Desc for "+name, "body")
	}
	catalog := skills.Discover(nil, userDir)
	if len(catalog) != 3 {
		t.Fatalf("len = %d", len(catalog))
	}
	want := []string{"alpha", "mid", "zeta"}
	for i, name := range want {
		if catalog[i].Name != name {
			t.Fatalf("catalog[%d] = %q, want %q", i, catalog[i].Name, name)
		}
	}
}

func TestReadRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "demo", "Demo skill for path checks", "body")
	skill := skills.Skill{
		Name:        "demo",
		Description: "Demo skill for path checks",
		Origin:      skills.OriginUser,
		Root:        filepath.Join(root, "demo"),
	}
	if _, err := skills.Read(skill, "../outside.md"); err == nil {
		t.Fatal("expected path traversal error")
	}
	if _, err := skills.Read(skill, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute path error")
	}
}

func TestReadReturnsAssetAndStripsFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "demo", "Demo skill", "skill body")
	assetDir := filepath.Join(root, "demo", "references")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "notes.md"), []byte("asset notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := skills.Skill{
		Name:        "demo",
		Description: "Demo skill",
		Origin:      skills.OriginUser,
		Root:        filepath.Join(root, "demo"),
	}

	body, err := skills.Read(skill, "")
	if err != nil {
		t.Fatal(err)
	}
	if body != "skill body" {
		t.Fatalf("SKILL.md body = %q, want skill body without frontmatter", body)
	}
	if strings.Contains(body, "---") || strings.Contains(body, "description:") {
		t.Fatalf("body still has frontmatter: %q", body)
	}

	asset, err := skills.Read(skill, "references/notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if asset != "asset notes\n" {
		t.Fatalf("asset = %q, want raw file contents", asset)
	}
}

func TestDiscoverCapsAt64(t *testing.T) {
	userDir := t.TempDir()
	for i := 0; i < skills.MaxCatalog+5; i++ {
		name := fmt.Sprintf("s-%03d", i)
		writeSkill(t, userDir, name, "Description for "+name, "body")
	}
	catalog := skills.Discover(nil, userDir)
	if len(catalog) != skills.MaxCatalog {
		t.Fatalf("len = %d, want %d", len(catalog), skills.MaxCatalog)
	}
}

func TestLookup(t *testing.T) {
	catalog := []skills.Skill{
		{Name: "alpha", Description: "A"},
		{Name: "beta", Description: "B"},
	}
	got, ok := skills.Lookup(catalog, " beta ")
	if !ok || got.Name != "beta" {
		t.Fatalf("Lookup = %+v ok=%v", got, ok)
	}
	if _, ok := skills.Lookup(catalog, "missing"); ok {
		t.Fatal("Lookup missing should be false")
	}
}
