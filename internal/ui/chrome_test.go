package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteChromePrintsHeaderPromptAndStatus(t *testing.T) {
	var output bytes.Buffer
	err := writeChrome(&output, REPLSettings{
		Version:        "0.0.1",
		Model:          "gpt-5.6-sol",
		PermissionMode: PermissionAsk,
		Effort:         "medium",
	})
	if err != nil {
		t.Fatalf("writeChrome() error = %v", err)
	}
	got := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	want := []string{
		"◆ gxx  0.0.1",
		">",
		"gpt-5.6-sol · ask · medium · 272k",
	}
	if len(got) != len(want) {
		t.Fatalf("chrome lines = %#v, want %#v", got, want)
	}
	for index, line := range want {
		if got[index] != line {
			t.Fatalf("line %d = %q, want %q", index+1, got[index], line)
		}
	}
}

func TestWriteAskHeaderOmitsPrompt(t *testing.T) {
	var output bytes.Buffer
	if err := WriteAskHeader(&output, REPLSettings{
		Version:        "0.0.1",
		Model:          "gpt-5.6-sol",
		PermissionMode: "ask",
		Effort:         "high",
		Color:          false,
	}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.HasPrefix(text, "◆ gxx  0.0.1\n") {
		t.Fatalf("header = %q", text)
	}
	if !strings.Contains(text, "gpt-5.6-sol · ask · high · 272k") {
		t.Fatalf("status missing from %q", text)
	}
	if strings.Contains(text, "\n>\n") {
		t.Fatalf("ask header should not include the prompt: %q", text)
	}
}

func TestFormatStatusAddsContextAndFast(t *testing.T) {
	plain := formatStatus(REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: PermissionAsk,
		Effort:         "medium",
	})
	if plain != "gpt-5.6-sol · ask · medium · 272k" {
		t.Fatalf("default status = %q", plain)
	}
	got := formatStatus(REPLSettings{
		Model:          "gpt-5.6-terra",
		PermissionMode: PermissionAsk,
		Effort:         "high",
		Context:        "1m",
		Fast:           true,
	})
	if got != "gpt-5.6-terra · ask · high · 1m · fast" {
		t.Fatalf("status = %q", got)
	}
}
