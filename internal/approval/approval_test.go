package approval

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
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
			got, err := prompt.Approve(context.Background(), Action{
				Title:   "Write file",
				Preview: "+content",
			})
			if err != nil {
				t.Fatalf("Approve() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Approve() = %v, want %v", got, test.want)
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
	approved, err := prompt.Approve(context.Background(), Action{Title: "Run"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if approved {
		t.Fatal("Approve() = true, want false")
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty", output.String())
	}
}

func TestPromptCanBeCancelledWhileWaiting(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prompt := testPrompt(bufio.NewReader(reader), io.Discard, true)
	approved, err := prompt.Approve(ctx, Action{Title: "Run"})
	if approved {
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
	approved, err := prompt.Approve(context.Background(), Action{Title: "Run"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if approved {
		t.Fatal("stale buffered input approved an action")
	}
}

func TestPromptEscapesTerminalControls(t *testing.T) {
	var output bytes.Buffer
	prompt := testPrompt(bufio.NewReader(strings.NewReader("n\n")), &output, true)
	if _, err := prompt.Approve(context.Background(), Action{
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

func TestPromptRejectsOversizedPreview(t *testing.T) {
	prompt := testPrompt(bufio.NewReader(strings.NewReader("y-test\n")), io.Discard, true)
	approved, err := prompt.Approve(context.Background(), Action{
		Title:   "Write",
		Preview: strings.Repeat("x", maxPreviewBytes+1),
	})
	if approved || err == nil {
		t.Fatalf("Approve() = %v, %v; want safe rejection", approved, err)
	}
}

func testPrompt(reader *bufio.Reader, writer io.Writer, interactive bool) *Prompt {
	prompt := NewPrompt(reader, writer, interactive)
	prompt.code = func() (string, error) { return "test", nil }
	return prompt
}
