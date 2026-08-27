package tools

import (
	"testing"
)

func TestIgnoreMatcherGitignoreAndGxxignore(t *testing.T) {
	matcher := &ignoreMatcher{}
	matcher.addFile(".", `
# comment
*.log
/generated
build/
!keep.log
`)
	matcher.addFile(".", "tmp/\n")

	tests := []struct {
		path string
		dir  bool
		want bool
	}{
		{path: "app.log", want: true},
		{path: "src/app.log", want: true},
		{path: "keep.log", want: false},
		{path: "coverage", dir: true, want: true},
		{path: "src/coverage", dir: true, want: true},
		{path: "generated", dir: true, want: true},
		{path: "src/generated", dir: true, want: false},
		{path: "build", dir: true, want: true},
		{path: "src/build", dir: true, want: true},
		{path: "tmp", dir: true, want: true},
		{path: "README.md", want: false},
		{path: "node_modules/pkg/index.js", dir: false, want: true},
	}
	for _, test := range tests {
		if got := matcher.ignores(test.path, test.dir); got != test.want {
			t.Errorf("ignores(%q, dir=%v) = %v, want %v", test.path, test.dir, got, test.want)
		}
	}
}

func TestIgnoreMatcherNestedGitignore(t *testing.T) {
	matcher := &ignoreMatcher{}
	matcher.addFile(".", "*.tmp")
	matcher.addFile("src", "secret.txt")
	if !matcher.ignores("src/secret.txt", false) {
		t.Fatal("nested gitignore should ignore src/secret.txt")
	}
	if matcher.ignores("secret.txt", false) {
		t.Fatal("nested gitignore should not apply at the workspace root")
	}
	if !matcher.ignores("src/foo.tmp", false) {
		t.Fatal("root gitignore should still match nested files")
	}
}

func TestIgnoreMatcherDoubleStar(t *testing.T) {
	matcher := &ignoreMatcher{}
	matcher.addFile(".", "**/*.pb.go\n")
	if !matcher.ignores("api/v1/foo.pb.go", false) {
		t.Fatal("**/*.pb.go should match nested generated files")
	}
	if matcher.ignores("api/v1/foo.go", false) {
		t.Fatal("**/*.pb.go should not match ordinary go files")
	}
}
