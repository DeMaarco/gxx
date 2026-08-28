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

package approval_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"gxx/internal/approval"
)

func TestPromptApprovesOnlyExplicitYes(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "y-test\n", want: true},
		{input: "Y-TEST\n", want: true},
		{input: "\n", want: false},
		{input: "no\n", want: false},
	}
	for _, test := range tests {
		t.Run(strings.TrimSpace(test.input), func(t *testing.T) {
			var output bytes.Buffer
			prompt := testPrompt(bufio.NewReader(strings.NewReader(test.input)), &output, true)
			got, err := prompt.Approve(context.Background(), approval.Action{
				Title:   "Write file",
				Preview: "+content",
			})
			if err != nil {
				t.Fatalf("Approve() error = %v", err)
			}
			if got.Approved != test.want {
				t.Fatalf("Approve() = %+v, want Approved %v", got, test.want)
			}
			if !strings.Contains(output.String(), "Type y-test") {
				t.Fatalf("output = %q, want prompt", output.String())
			}
		})
	}
}

func TestNonInteractivePromptDeniesWithoutReading(t *testing.T) {
	var output bytes.Buffer
	prompt := testPrompt(nil, &output, false)
	decision, err := prompt.Approve(context.Background(), approval.Action{Title: "Run"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if decision.Approved {
		t.Fatal("Approve() = true, want false")
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty", output.String())
	}
}

func TestPromptCancelDoesNotConsumeNextLine(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	prompt := testPrompt(bufio.NewReader(reader), io.Discard, true)
	prompt.SetFile(reader)

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := prompt.Approve(ctx, approval.Action{Title: "Run"})
		done <- err
	}()
	<-started
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Approve() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Approve() did not return after cancel")
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("y-test\n"))
		writeDone <- err
	}()
	decision, err := prompt.Approve(context.Background(), approval.Action{Title: "Next"})
	if err != nil {
		t.Fatalf("second Approve() error = %v", err)
	}
	if !decision.Approved {
		t.Fatal("cancelled read consumed the next approval line")
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write approval: %v", err)
	}
}

func TestPromptCanBeCancelledWhileWaiting(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prompt := testPrompt(bufio.NewReader(reader), io.Discard, true)
	decision, err := prompt.Approve(ctx, approval.Action{Title: "Run"})
	if decision.Approved {
		t.Fatal("Approve() = true, want false")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Approve() error = %v, want context.Canceled", err)
	}
}

func TestPromptDiscardsInputBufferedBeforePreview(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("request\ny-test\n"))
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	prompt := testPrompt(reader, io.Discard, true)
	decision, err := prompt.Approve(context.Background(), approval.Action{Title: "Run"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if decision.Approved {
		t.Fatal("stale buffered input approved an action")
	}
}

func TestPromptEscapesTerminalControls(t *testing.T) {
	var output bytes.Buffer
	prompt := testPrompt(bufio.NewReader(strings.NewReader("n\n")), &output, true)
	if _, err := prompt.Approve(context.Background(), approval.Action{
		Title:   "Run \x1b[2J",
		Preview: "danger\rhidden\u202ereversed",
	}); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if strings.Contains(output.String(), "\x1b") ||
		strings.Contains(output.String(), "\r") ||
		strings.Contains(output.String(), "\u202e") {
		t.Fatalf("output contains raw terminal controls: %q", output.String())
	}
	if !strings.Contains(output.String(), `\u001b`) ||
		!strings.Contains(output.String(), `\r`) ||
		!strings.Contains(output.String(), `\u202e`) {
		t.Fatalf("output = %q, want escaped controls", output.String())
	}
}

func TestPromptTruncatesOversizedPreview(t *testing.T) {
	var output bytes.Buffer
	prompt := testPrompt(bufio.NewReader(strings.NewReader("y-test\n")), &output, true)
	decision, err := prompt.Approve(context.Background(), approval.Action{
		Title:   "Write",
		Preview: strings.Repeat("x", approval.MaxPreviewBytes+1),
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if !decision.Approved {
		t.Fatal("Approve() = false, want truncated preview to still be approvable")
	}
	text := output.String()
	if !strings.Contains(text, "preview truncated") {
		t.Fatalf("output = %q, want truncation marker", text)
	}
	if strings.Count(text, "x") < approval.MaxPreviewBytes {
		t.Fatalf("output showed %d x runes, want at least %d", strings.Count(text, "x"), approval.MaxPreviewBytes)
	}
}

func TestPromptSessionAllowRemembersOnlyWithRepeatKey(t *testing.T) {
	var output bytes.Buffer
	prompt := testPrompt(bufio.NewReader(strings.NewReader("a-test\n")), &output, true)
	decision, err := prompt.Approve(context.Background(), approval.Action{
		Title:     "Run command",
		Preview:   "$ go test ./...",
		Kind:      approval.KindCommand,
		RepeatKey: "go test ./...",
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if !decision.Approved || !decision.Remember {
		t.Fatalf("decision = %+v, want approved and remember", decision)
	}
	if !strings.Contains(output.String(), "a-test to allow this command for the session") {
		t.Fatalf("output = %q, want session allow prompt", output.String())
	}
}

func TestPromptSessionAllowWithoutRepeatKeyIsDenied(t *testing.T) {
	prompt := testPrompt(bufio.NewReader(strings.NewReader("a-test\n")), io.Discard, true)
	decision, err := prompt.Approve(context.Background(), approval.Action{
		Title:   "Write file",
		Preview: "+content",
		Kind:    approval.KindWrite,
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if decision.Approved || decision.Remember {
		t.Fatalf("decision = %+v, want denied write", decision)
	}
}

func TestPromptWriteDoesNotOfferSessionAllow(t *testing.T) {
	var output bytes.Buffer
	prompt := testPrompt(bufio.NewReader(strings.NewReader("y-test\n")), &output, true)
	if _, err := prompt.Approve(context.Background(), approval.Action{
		Title:   "Write file",
		Preview: "+content",
		Kind:    approval.KindWrite,
	}); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if strings.Contains(output.String(), "allow this command") {
		t.Fatalf("write prompt offered session allow: %q", output.String())
	}
}

func TestCapPreviewCutsWithoutBlocking(t *testing.T) {
	got := approval.CapPreview(strings.Repeat("a", approval.MaxPreviewBytes+8))
	if len(got) != approval.MaxPreviewBytes {
		t.Fatalf("len(CapPreview()) = %d, want %d", len(got), approval.MaxPreviewBytes)
	}
}

func testPrompt(reader *bufio.Reader, writer io.Writer, interactive bool) *approval.Prompt {
	prompt := approval.NewPrompt(reader, writer, interactive)
	prompt.SetCode(func() (string, error) { return "test", nil })
	return prompt
}
