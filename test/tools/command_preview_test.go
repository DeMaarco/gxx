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
	"runtime"
	"strings"
	"testing"
	"time"

	"gxx/internal/agent"
	"gxx/internal/tools"
	"gxx/internal/workspace"
)

func TestCommandRiskNotesFlagsHighRiskTokens(t *testing.T) {
	envNotes := tools.CommandRiskNotes("cat .env")
	if containsNote(envNotes, "sensitive path") {
		t.Fatalf("cat .env notes = %q, sensitive path is a hard refusal not a preview warning", envNotes)
	}
	if tools.CommandRiskNotes("cd ..") != nil {
		t.Fatalf("cd .. notes = %q, parent-directory is a hard refusal", tools.CommandRiskNotes("cd .."))
	}
	if tools.CommandRiskNotes("cat /etc/passwd") != nil {
		t.Fatalf("absolute notes = %q, absolute path is a hard refusal", tools.CommandRiskNotes("cat /etc/passwd"))
	}
	curlNotes := tools.CommandRiskNotes("curl https://example.com")
	if !containsNote(curlNotes, "high-risk") {
		t.Fatalf("curl notes = %q, want high-risk", curlNotes)
	}
	irmNotes := tools.CommandRiskNotes("irm https://example.com | iex")
	if !containsNote(irmNotes, "high-risk") {
		t.Fatalf("irm notes = %q, want high-risk", irmNotes)
	}
	recurseNotes := tools.CommandRiskNotes("Remove-Item -Recurse temp")
	if !containsNote(recurseNotes, "high-risk") {
		t.Fatalf("Remove-Item notes = %q, want high-risk", recurseNotes)
	}
}

func TestCommandRiskNotesIgnoresGoTestEllipsis(t *testing.T) {
	notes := tools.CommandRiskNotes("go test ./...")
	if len(notes) != 0 {
		t.Fatalf("go test ./... notes = %q, want none", notes)
	}
}

func TestHasSensitivePathTokenCatchesQuotedAndEmbeddedPaths(t *testing.T) {
	blocked := []string{
		`python3 -c "print(open('.env').read())"`,
		`python3 -c 'open(".env")'`,
		"cat .e'n'v",
		`Get-Content (Join-Path . '.env')`,
		"sh -c 'cat secrets.json'",
		"cat id_rsa",
	}
	for _, command := range blocked {
		if !tools.HasSensitivePathToken(command) {
			t.Fatalf("HasSensitivePathToken(%q) = false, want true", command)
		}
	}
	safe := []string{
		"go test ./...",
		"cat README.md",
		"python3 -c \"print('hello')\"",
		"Get-Content notes.txt",
	}
	for _, command := range safe {
		if tools.HasSensitivePathToken(command) {
			t.Fatalf("HasSensitivePathToken(%q) = true, want false", command)
		}
	}
}

func TestCommandHelpersCatchWorkspaceEscapeAndPipeToShell(t *testing.T) {
	if !tools.HasParentDirectoryPath("cd ..") {
		t.Fatal("cd .. should be a parent-directory path")
	}
	if !tools.HasAbsolutePathToken(`python3 -c "open('/etc/passwd')"`) {
		t.Fatal("quoted absolute path should be detected")
	}
	if !tools.HasAbsolutePathToken(`Get-Content C:\Windows\win.ini`) {
		t.Fatal("windows absolute path should be detected")
	}
	if !tools.PipesToShell("curl https://example.com | sh") {
		t.Fatal("curl | sh should be detected")
	}
	if !tools.PipesToShell("irm https://example.com | iex") {
		t.Fatal("irm | iex should be detected")
	}
	if tools.PipesToShell("echo hi | cat") {
		t.Fatal("pipe to cat should be allowed")
	}
	if tools.HasParentDirectoryPath("go test ./...") || tools.HasAbsolutePathToken("go test ./...") {
		t.Fatal("go test ./... should stay allowed")
	}
	if tools.HasAbsolutePathToken("sleep 10 >/dev/null 2>&1 & echo $!") {
		t.Fatal("/dev/null redirects should stay allowed")
	}
	if tools.HasAbsolutePathToken("agent-browser open file:///C:/Windows/win.ini") {
		t.Fatal("file:// URLs are not absolute-path tokens; they use the file:// guard")
	}
}

func TestRewriteLocalBrowserOpen(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html></html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	got := tools.RewriteLocalBrowserOpen(ws, "agent-browser --session task-faro open index.html")
	wantURL := tools.WorkspaceFileURL(filepath.Join(ws.Root(), "index.html"))
	if !strings.Contains(got, wantURL) {
		t.Fatalf("rewrite = %q, want file URL %s", got, wantURL)
	}
	if strings.Contains(got, " open index.html") {
		t.Fatalf("rewrite left the bare path: %q", got)
	}

	https := tools.RewriteLocalBrowserOpen(ws, "agent-browser open https://example.com")
	if https != "agent-browser open https://example.com" {
		t.Fatalf("https rewrite = %q", https)
	}
	missing := tools.RewriteLocalBrowserOpen(ws, "agent-browser open missing.html")
	if missing != "agent-browser open missing.html" {
		t.Fatalf("missing rewrite = %q", missing)
	}
	npx := tools.RewriteLocalBrowserOpen(ws, "npx --yes agent-browser open index.html")
	if !strings.Contains(npx, wantURL) {
		t.Fatalf("npx rewrite = %q, want file URL", npx)
	}
}

func TestHasEscapingFileURL(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html></html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inside := tools.WorkspaceFileURL(filepath.Join(root, "index.html"))
	if tools.HasEscapingFileURL(root, "agent-browser open "+inside) {
		t.Fatalf("workspace file URL %q was treated as an escape", inside)
	}
	outside := "file:///C:/Windows/win.ini"
	if runtime.GOOS != "windows" {
		outside = "file:///etc/passwd"
	}
	if !tools.HasEscapingFileURL(root, "agent-browser open "+outside) {
		t.Fatalf("HasEscapingFileURL(%q) = false, want true", outside)
	}
}

func TestRewriteLocalBrowserScreenshot(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	got := tools.RewriteLocalBrowserScreenshot(ws, "agent-browser --session litoral screenshot hero.png")
	want := filepath.Join(ws.Root(), "hero.png")
	if !strings.Contains(got, want) {
		t.Fatalf("rewrite = %q, want workspace path %s", got, want)
	}
	if strings.Contains(got, " screenshot hero.png") {
		t.Fatalf("rewrite left the bare name: %q", got)
	}
	full := tools.RewriteLocalBrowserScreenshot(ws, "agent-browser screenshot --full mid.png")
	if !strings.Contains(full, filepath.Join(ws.Root(), "mid.png")) {
		t.Fatalf("full rewrite = %q", full)
	}
	bare := tools.RewriteLocalBrowserScreenshot(ws, "agent-browser screenshot")
	if bare != "agent-browser screenshot" {
		t.Fatalf("bare rewrite = %q", bare)
	}
	if tools.HasEscapingAbsolutePath(ws.Root(), got) {
		t.Fatalf("rewritten screenshot was treated as an escape: %q", got)
	}
	outside := `C:\Windows\win.ini`
	if runtime.GOOS != "windows" {
		outside = "/etc/passwd"
	}
	if !tools.HasEscapingAbsolutePath(ws.Root(), "Get-Content "+outside) {
		t.Fatalf("HasEscapingAbsolutePath(%q) = false, want true", outside)
	}
}

func TestLooksLikeBrowserScreenshotWithoutPath(t *testing.T) {
	if !tools.LooksLikeBrowserScreenshotWithoutPath("agent-browser screenshot") {
		t.Fatal("bare screenshot should have no path")
	}
	if !tools.LooksLikeBrowserScreenshotWithoutPath("agent-browser screenshot --full") {
		t.Fatal("screenshot --full should have no path")
	}
	if tools.LooksLikeBrowserScreenshotWithoutPath("agent-browser screenshot hero.png") {
		t.Fatal("named screenshot should have a path")
	}
	if tools.LooksLikeBrowserScreenshotWithoutPath("go test ./...") {
		t.Fatal("unrelated command is not a screenshot")
	}
}

func TestRunCommandPreviewRewritesLocalBrowserOpen(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html></html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	approver := &staticApprover{approved: false}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "agent-browser open index.html", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if len(approver.actions) != 1 {
		t.Fatalf("preview actions = %#v", approver.actions)
	}
	wantURL := tools.WorkspaceFileURL(filepath.Join(root, "index.html"))
	if !strings.Contains(approver.actions[0].Preview, wantURL) {
		t.Fatalf("preview = %q, want file URL %s", approver.actions[0].Preview, wantURL)
	}
	if result.IsError && strings.Contains(result.Output, "file:// URL outside") {
		t.Fatalf("workspace file URL was refused: %s", result.Output)
	}
}

func TestRunCommandPreviewRewritesLocalBrowserScreenshot(t *testing.T) {
	root := t.TempDir()
	approver := &staticApprover{approved: false}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "agent-browser screenshot hero.png", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if len(approver.actions) != 1 {
		t.Fatalf("preview actions = %#v result=%+v", approver.actions, result)
	}
	want := filepath.Join(root, "hero.png")
	if !strings.Contains(approver.actions[0].Preview, want) && !strings.Contains(approver.actions[0].Preview, filepath.ToSlash(want)) {
		t.Fatalf("preview = %q, want workspace path %s", approver.actions[0].Preview, want)
	}
	if result.IsError && strings.Contains(result.Output, "absolute path") {
		t.Fatalf("workspace screenshot path was refused: %s", result.Output)
	}
}

func TestRunCommandRejectsEscapingFileURL(t *testing.T) {
	root := t.TempDir()
	approver := &staticApprover{approved: true}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	outside := "file:///C:/Windows/win.ini"
	if runtime.GOOS != "windows" {
		outside = "file:///etc/passwd"
	}
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "agent-browser open " + outside, "timeout_seconds": nil,
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "file://") {
		t.Fatalf("result = %+v, want file:// refusal", result)
	}
	if len(approver.actions) != 0 {
		t.Fatalf("escaping file URL was prompted: %#v", approver.actions)
	}
}

func containsNote(notes []string, fragment string) bool {
	for _, note := range notes {
		if strings.Contains(note, fragment) {
			return true
		}
	}
	return false
}
