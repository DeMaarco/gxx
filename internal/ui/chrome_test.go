package ui

import (
	"bytes"
	"strings"
	"testing"

	"gxx/internal/config"
)

func TestWriteChromePrintsHeaderPromptAndStatus(t *testing.T) {
	var output bytes.Buffer
	err := writeChrome(&output, REPLSettings{
		Version:        "0.0.1",
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAsk,
		Effort:         "medium",
	})
	if err != nil {
		t.Fatalf("writeChrome() error = %v", err)
	}
	got := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	want := []string{
		"◆ gxx  v0.0.1",
		">",
		"gpt-5.6-sol · ask · medium · 272k · 0%",
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
	if !strings.HasPrefix(text, "◆ gxx  v0.0.1\n") {
		t.Fatalf("header = %q", text)
	}
	if !strings.Contains(text, "gpt-5.6-sol · ask · high · 272k · 0%") {
		t.Fatalf("status missing from %q", text)
	}
	if strings.Contains(text, "\n>\n") {
		t.Fatalf("ask header should not include the prompt: %q", text)
	}
}

func TestFormatStatusAddsContextAndFast(t *testing.T) {
	plain := formatStatus(REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAsk,
		Effort:         "medium",
	})
	if plain != "gpt-5.6-sol · ask · medium · 272k · 0%" {
		t.Fatalf("default status = %q", plain)
	}
	got := formatStatus(REPLSettings{
		Model:          "gpt-5.6-terra",
		PermissionMode: config.PermissionAsk,
		Effort:         "high",
		Context:        "1m",
		Fast:           true,
	})
	if got != "gpt-5.6-terra · ask · high · 1m · 0% · fast" {
		t.Fatalf("status = %q", got)
	}
}

func TestFormatStatusPaintsAutoRed(t *testing.T) {
	plain := formatStatus(REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAuto,
		Effort:         "medium",
	})
	if plain != "gpt-5.6-sol · auto · medium · 272k · 0%" {
		t.Fatalf("plain auto status = %q", plain)
	}
	got := formatStatus(REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAuto,
		Effort:         "medium",
		Color:          true,
	})
	if !strings.Contains(got, paint(true, bold+red, "auto")) {
		t.Fatalf("colored auto status = %q, want red auto", got)
	}
	if strings.Contains(got, paint(true, bold+red, "ask")) {
		t.Fatalf("colored auto status painted ask: %q", got)
	}
}

func TestFormatStatusKeepsAutoWritesPlain(t *testing.T) {
	got := formatStatus(REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAutoWrites,
		Effort:         "medium",
		Color:          true,
	})
	if strings.Contains(got, paint(true, red, "auto-writes")) ||
		strings.Contains(got, paint(true, bold+red, "auto-writes")) {
		t.Fatalf("auto-writes should not be red: %q", got)
	}
	if !strings.Contains(got, "auto-writes") {
		t.Fatalf("status = %q, want auto-writes", got)
	}
}

func TestFormatVersionPrefixesRelease(t *testing.T) {
	if got := formatVersion("0.0.1"); got != "v0.0.1" {
		t.Fatalf("formatVersion(0.0.1) = %q", got)
	}
	if got := formatVersion("v0.0.1"); got != "v0.0.1" {
		t.Fatalf("formatVersion(v0.0.1) = %q", got)
	}
	if got := formatVersion(""); got != "dev" {
		t.Fatalf("formatVersion(empty) = %q", got)
	}
}
