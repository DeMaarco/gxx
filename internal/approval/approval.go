package approval

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"
)

const maxPreviewBytes = 16 * 1024

type Kind string

const (
	KindWrite   Kind = "write"
	KindCommand Kind = "command"
)

type Action struct {
	Title   string
	Preview string
	Kind    Kind
}

type Approver interface {
	Approve(context.Context, Action) (bool, error)
}

// Prompt asks for explicit y/N approval. Non-interactive instances deny.
type Prompt struct {
	reader      *bufio.Reader
	writer      io.Writer
	interactive bool
	code        func() (string, error)
	mu          sync.Mutex
}

func NewPrompt(reader *bufio.Reader, writer io.Writer, interactive bool) *Prompt {
	return &Prompt{
		reader:      reader,
		writer:      writer,
		interactive: interactive,
		code:        confirmationCode,
	}
}

func (p *Prompt) Approve(ctx context.Context, action Action) (bool, error) {
	if !p.interactive {
		return false, nil
	}
	if p.reader == nil || p.writer == nil {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if len(action.Preview) > maxPreviewBytes {
		return false, fmt.Errorf(
			"action preview is %d bytes; refusing to approve more than %d bytes",
			len(action.Preview),
			maxPreviewBytes,
		)
	}
	if buffered := p.reader.Buffered(); buffered > 0 {
		if _, err := p.reader.Discard(buffered); err != nil {
			return false, fmt.Errorf("discard stale terminal input: %w", err)
		}
	}
	code, err := p.code()
	if err != nil {
		return false, fmt.Errorf("create approval challenge: %w", err)
	}
	title := safeDisplay(action.Title)
	preview := safeDisplay(action.Preview)
	if strings.TrimSpace(preview) != "" {
		if _, err := fmt.Fprintf(p.writer, "\n%s\n%s\n", title, preview); err != nil {
			return false, err
		}
	} else if _, err := fmt.Fprintf(p.writer, "\n%s\n", title); err != nil {
		return false, err
	}
	if _, err := fmt.Fprintf(
		p.writer,
		"Approve? Type y-%s to approve [default N]: ",
		code,
	); err != nil {
		return false, err
	}

	type readResult struct {
		answer string
		err    error
	}
	read := make(chan readResult, 1)
	go func() {
		answer, err := p.reader.ReadString('\n')
		read <- readResult{answer: answer, err: err}
	}()

	var answer string
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case result := <-read:
		if result.err != nil && result.err != io.EOF {
			return false, result.err
		}
		answer = result.answer
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y-"+code, nil
}

func safeDisplay(value string) string {
	var output strings.Builder
	for _, character := range value {
		switch character {
		case '\n', '\t':
			output.WriteRune(character)
		case '\r':
			output.WriteString(`\r`)
		default:
			if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
				writeEscapedRune(&output, character)
			} else {
				output.WriteRune(character)
			}
		}
	}
	return output.String()
}

func writeEscapedRune(output *strings.Builder, character rune) {
	if character <= 0xffff {
		fmt.Fprintf(output, `\u%04x`, character)
		return
	}
	fmt.Fprintf(output, `\U%08x`, character)
}

func confirmationCode() (string, error) {
	value := make([]byte, 2)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
