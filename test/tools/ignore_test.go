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
	"testing"

	"gxx/internal/tools"
)

func TestIgnoreMatcherGitignoreAndGxxignore(t *testing.T) {
	matcher := &tools.IgnoreMatcher{}
	matcher.AddFile(".", `
# comment
*.log
/generated
build/
!keep.log
`)
	matcher.AddFile(".", "tmp/\n")

	tests := []struct {
		path string
		dir  bool
		want bool
	}{
		{path: "app.log", want: true},
		{path: "src/app.log", want: true},
		{path: "keep.log", want: false},
		{path: "coverage", dir: true, want: false},
		{path: "src/coverage", dir: true, want: false},
		{path: "generated", dir: true, want: true},
		{path: "src/generated", dir: true, want: false},
		{path: "build", dir: true, want: true},
		{path: "src/build", dir: true, want: true},
		{path: "tmp", dir: true, want: true},
		{path: "README.md", want: false},
		{path: "node_modules/pkg/index.js", dir: false, want: false},
	}
	for _, test := range tests {
		if got := matcher.Ignores(test.path, test.dir); got != test.want {
			t.Errorf("ignores(%q, dir=%v) = %v, want %v", test.path, test.dir, got, test.want)
		}
	}
}

func TestIgnoreMatcherNestedGitignore(t *testing.T) {
	matcher := &tools.IgnoreMatcher{}
	matcher.AddFile(".", "*.tmp")
	matcher.AddFile("src", "secret.txt")
	if !matcher.Ignores("src/secret.txt", false) {
		t.Fatal("nested gitignore should ignore src/secret.txt")
	}
	if matcher.Ignores("secret.txt", false) {
		t.Fatal("nested gitignore should not apply at the workspace root")
	}
	if !matcher.Ignores("src/foo.tmp", false) {
		t.Fatal("root gitignore should still match nested files")
	}
}

func TestIgnoreMatcherDoubleStar(t *testing.T) {
	matcher := &tools.IgnoreMatcher{}
	matcher.AddFile(".", "**/*.pb.go\n")
	if !matcher.Ignores("api/v1/foo.pb.go", false) {
		t.Fatal("**/*.pb.go should match nested generated files")
	}
	if matcher.Ignores("api/v1/foo.go", false) {
		t.Fatal("**/*.pb.go should not match ordinary go files")
	}
}

func TestDefaultIgnorePatternsSkipDependencyDirs(t *testing.T) {
	matcher := tools.NewIgnoreMatcher()
	matcher.AddFile(".", tools.DefaultIgnorePatterns)
	if !matcher.Ignores("node_modules/pkg/index.js", false) {
		t.Fatal("node_modules should be ignored by default")
	}
	if !matcher.Ignores(".venv/bin/python", false) {
		t.Fatal(".venv should be ignored by default")
	}
	if matcher.Ignores("vendor/pkg.go", false) {
		t.Fatal("vendor should not be ignored by default")
	}
	if matcher.Ignores("bin/tool.go", false) {
		t.Fatal("bin should not be ignored by default")
	}
	if matcher.Ignores("build/output", true) {
		t.Fatal("build should not be ignored by default")
	}
}

func TestDefaultIgnorePatternsCanBeNegated(t *testing.T) {
	matcher := tools.NewIgnoreMatcher()
	matcher.AddFile(".", tools.DefaultIgnorePatterns)
	matcher.AddFile(".", "!node_modules/\n")
	if matcher.Ignores("node_modules/pkg/index.js", false) {
		t.Fatal("!node_modules/ should un-ignore the default pattern")
	}
}

func TestGitDirectoryIsAlwaysIgnored(t *testing.T) {
	matcher := tools.NewIgnoreMatcher()
	matcher.AddFile(".", "!**/.git/\n")
	if !matcher.Ignores(".git/config", false) {
		t.Fatal(".git must stay ignored even with a negation")
	}
}
