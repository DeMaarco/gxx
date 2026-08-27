package tools

import (
	"strings"
	"testing"
)

func TestParsePatchSupportsMultipleFileOperations(t *testing.T) {
	patch := `*** Begin Patch
*** Add File: new.txt
+new file
*** Update File: existing.txt
@@
 before
-old
+new
 after
*** Delete File: obsolete.txt
*** End Patch`

	operations, err := parsePatch(patch)
	if err != nil {
		t.Fatalf("parsePatch() error = %v", err)
	}
	if len(operations) != 3 {
		t.Fatalf("len(operations) = %d, want 3", len(operations))
	}
	if operations[0].kind != patchAdd || string(operations[0].data) != "new file\n" {
		t.Fatalf("add operation = %+v, data=%q", operations[0], operations[0].data)
	}
	if operations[1].kind != patchUpdate || len(operations[1].hunks) != 1 {
		t.Fatalf("update operation = %+v", operations[1])
	}
	if operations[2].kind != patchDelete {
		t.Fatalf("delete operation = %+v", operations[2])
	}
}

func TestApplyPatchHunksUsesExactContext(t *testing.T) {
	operations, err := parsePatch(`*** Begin Patch
*** Update File: main.go
@@
 package main
 
-func old() {}
+func current() {}
*** End Patch`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := applyPatchHunks(
		[]byte("package main\n\nfunc old() {}\n"),
		operations[0],
	)
	if err != nil {
		t.Fatalf("applyPatchHunks() error = %v", err)
	}
	if string(got) != "package main\n\nfunc current() {}\n" {
		t.Fatalf("updated contents = %q", got)
	}
}

func TestApplyPatchHunksRejectsAmbiguousContext(t *testing.T) {
	operations, err := parsePatch(`*** Begin Patch
*** Update File: repeated.txt
@@
-same
+changed
*** End Patch`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyPatchHunks([]byte("same\nsame\n"), operations[0])
	if err == nil || !strings.Contains(err.Error(), "found 2 matches") {
		t.Fatalf("applyPatchHunks() error = %v, want ambiguous-match error", err)
	}
}

func TestParsePatchPreservesMissingFinalNewline(t *testing.T) {
	operations, err := parsePatch(`*** Begin Patch
*** Update File: file.txt
@@
-before
+after
*** End of File
*** End Patch`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := applyPatchHunks([]byte("before"), operations[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "after" {
		t.Fatalf("updated contents = %q, want no final newline", got)
	}
}

func TestApplyPatchHunksIgnoresSubstringInsideLongerLine(t *testing.T) {
	operations, err := parsePatch(`*** Begin Patch
*** Update File: file.txt
@@
-x
+y
*** End Patch`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := applyPatchHunks([]byte("ax\nx\n"), operations[0])
	if err != nil {
		t.Fatalf("applyPatchHunks() error = %v", err)
	}
	if string(got) != "ax\ny\n" {
		t.Fatalf("updated contents = %q, want the complete line replaced", got)
	}
}

func TestApplyPatchHunksRejectsPartialLineMatch(t *testing.T) {
	operations, err := parsePatch(`*** Begin Patch
*** Update File: file.txt
@@
-x
+y
*** End Patch`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyPatchHunks([]byte("ax\n"), operations[0])
	if err == nil || !strings.Contains(err.Error(), "found 0 matches") {
		t.Fatalf("applyPatchHunks() error = %v, want no complete-line match", err)
	}
}

func TestApplyPatchHunksRejectsNoEOLPrefixOfLastLine(t *testing.T) {
	operations, err := parsePatch(`*** Begin Patch
*** Update File: file.txt
@@
-bar
+qux
*** End of File
*** End Patch`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyPatchHunks([]byte("barbaz"), operations[0])
	if err == nil || !strings.Contains(err.Error(), "found 0 matches") {
		t.Fatalf("applyPatchHunks() error = %v, want last-line prefix rejection", err)
	}
}

func TestParsePatchRejectsDuplicatePaths(t *testing.T) {
	_, err := parsePatch(`*** Begin Patch
*** Add File: same.txt
+one
*** Delete File: same.txt
*** End Patch`)
	if err == nil || !strings.Contains(err.Error(), "duplicate path") {
		t.Fatalf("parsePatch() error = %v, want duplicate path", err)
	}
}
