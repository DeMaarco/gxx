package test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestInternalPackagesDoNotContainTestFiles(t *testing.T) {
	root := filepath.Join("..", "internal")
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), "_test.go") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("internal test files must live under test/: %s", strings.Join(found, ", "))
	}
}
