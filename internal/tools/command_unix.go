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

//go:build unix

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func (r *Registry) runCommand(ctx context.Context, raw json.RawMessage) (string, error) {
	args, timeout, err := r.parseCommandArgs(raw)
	if err != nil {
		return "", err
	}

	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.Command("/bin/sh", "-c", args.Command)
	command.Dir = r.workspace.Root()
	command.Env = sanitizedEnvironment(os.Environ())
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second

	output := &limitedWriter{limit: r.maxResultBytes * 2}
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}

	waited := make(chan error, 1)
	go func() {
		waited <- command.Wait()
	}()

	select {
	case err := <-waited:
		// The shell may have launched background children and exited. They
		// remain in its process group, so terminate the group before returning.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		text := output.String()
		if err == nil {
			if strings.TrimSpace(text) == "" {
				return "Command completed successfully.", nil
			}
			return text, nil
		}
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return text, fmt.Errorf("command failed: %w", err)
		}
		return withMissingCommandHint(args.Command, reportExit(exitError, text)), nil
	case <-commandContext.Done():
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-waited
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			return output.String(), fmt.Errorf("command timed out after %s", timeout)
		}
		return output.String(), commandContext.Err()
	}
}

func signaledExitLabel(exitError *exec.ExitError) string {
	if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return fmt.Sprintf("%s %d (%s)", exitCodeLabel, 128+int(status.Signal()), status.Signal())
	}
	return ""
}
