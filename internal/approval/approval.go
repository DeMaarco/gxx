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

package approval

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"gxx/internal/osutil"
)

const maxPreviewBytes = 16 * 1024

type Kind string

const (
	KindWrite   Kind = "write"
	KindCommand Kind = "command"
)

type Action struct {
	Title     string
	Preview   string
	Kind      Kind
	RepeatKey string
}

// Decision is the result of an approval prompt.
type Decision struct {
	Approved bool
	Remember bool // a-xxxx: remember RepeatKey for this session
}

type Approver interface {
	Approve(context.Context, Action) (Decision, error)
}

// Prompt asks for explicit y/N approval. Non-interactive instances deny.
type Prompt struct {
	reader      *bufio.Reader
	writer      io.Writer
	file        *os.File
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

func (p *Prompt) SetFile(file *os.File) {
	p.file = file
}

func (p *Prompt) Approve(ctx context.Context, action Action) (Decision, error) {
	if !p.interactive {
		return Decision{}, nil
	}
	if p.reader == nil || p.writer == nil {
		return Decision{}, nil
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if buffered := p.reader.Buffered(); buffered > 0 {
		if _, err := p.reader.Discard(buffered); err != nil {
			return Decision{}, fmt.Errorf("discard stale terminal input: %w", err)
		}
	}
	code, err := p.code()
	if err != nil {
		return Decision{}, fmt.Errorf("create approval challenge: %w", err)
	}
	title := safeDisplay(action.Title)
	preview := displayPreview(action.Preview)
	if strings.TrimSpace(preview) != "" {
		if _, err := fmt.Fprintf(p.writer, "\n%s\n%s\n", title, preview); err != nil {
			return Decision{}, err
		}
	} else if _, err := fmt.Fprintf(p.writer, "\n%s\n", title); err != nil {
		return Decision{}, err
	}
	if action.RepeatKey != "" {
		if _, err := fmt.Fprintf(
			p.writer,
			"Approve? Type y-%s to approve, a-%s to allow this command for the session [default N]: ",
			code,
			code,
		); err != nil {
			return Decision{}, err
		}
	} else if _, err := fmt.Fprintf(
		p.writer,
		"Approve? Type y-%s to approve [default N]: ",
		code,
	); err != nil {
		return Decision{}, err
	}

	answer, err := readLineContext(ctx, p.reader, p.file)
	if err != nil && err != io.EOF {
		return Decision{}, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	switch answer {
	case "y-" + code:
		return Decision{Approved: true}, nil
	case "a-" + code:
		if action.RepeatKey == "" {
			return Decision{}, nil
		}
		return Decision{Approved: true, Remember: true}, nil
	default:
		return Decision{}, nil
	}
}

func readLineContext(ctx context.Context, reader *bufio.Reader, file *os.File) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if file != nil {
		stop := context.AfterFunc(ctx, func() {
			osutil.InterruptRead(file)
		})
		defer stop()
		defer osutil.ClearReadDeadline(file)
		line, err := reader.ReadString('\n')
		if err != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}
		return line, err
	}

	type readResult struct {
		answer string
		err    error
	}
	read := make(chan readResult, 1)
	go func() {
		answer, err := reader.ReadString('\n')
		read <- readResult{answer: answer, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-read:
		return result.answer, result.err
	}
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

// CapPreview limits a raw action preview so callers do not keep huge diffs in memory.
func CapPreview(preview string) string {
	if len(preview) <= maxPreviewBytes {
		return preview
	}
	keep := maxPreviewBytes
	for keep > 0 && !utf8.RuneStart(preview[keep]) {
		keep--
	}
	return preview[:keep]
}

func displayPreview(preview string) string {
	shown := safeDisplay(preview)
	n := len(shown)
	if n <= maxPreviewBytes {
		return shown
	}
	keep := maxPreviewBytes
	for keep > 0 && !utf8.RuneStart(shown[keep]) {
		keep--
	}
	return shown[:keep] + fmt.Sprintf("\n… preview truncated (%d of %d bytes shown)", keep, n)
}
