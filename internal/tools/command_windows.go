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

//go:build windows

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
	"unsafe"

	"golang.org/x/sys/windows"
)

func (r *Registry) runCommand(ctx context.Context, raw json.RawMessage) (string, error) {
	args, timeout, err := r.parseCommandArgs(raw)
	if err != nil {
		return "", err
	}

	shell, err := lookPowerShell()
	if err != nil {
		return "", err
	}

	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", args.Command)
	command.Dir = r.workspace.Root()
	command.Env = sanitizedEnvironment(os.Environ())
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
	command.WaitDelay = 2 * time.Second

	output := &limitedWriter{limit: r.maxResultBytes * 2}
	command.Stdout = output
	command.Stderr = output

	job, err := newKillJob()
	if err != nil {
		return "", fmt.Errorf("create process job: %w", err)
	}
	defer windows.CloseHandle(job)

	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}
	if err := assignPIDToJob(job, command.Process.Pid); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = command.Process.Kill()
		return "", fmt.Errorf("confine command process: %w", err)
	}

	// Kill the job as soon as PowerShell exits so leftover children cannot
	// hold stdout/stderr open and stall exec.Cmd.Wait (WaitDelay).
	go terminateJobWhenProcessExits(job, command.Process.Pid)

	waited := make(chan error, 1)
	go func() {
		waited <- command.Wait()
	}()

	select {
	case err := <-waited:
		_ = windows.TerminateJobObject(job, 1)
		return finishWindowsCommand(output.String(), err, command.ProcessState)
	case <-commandContext.Done():
		_ = windows.TerminateJobObject(job, 1)
		<-waited
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			return output.String(), fmt.Errorf("command timed out after %s", timeout)
		}
		return output.String(), commandContext.Err()
	}
}

func finishWindowsCommand(text string, err error, state *os.ProcessState) (string, error) {
	if err == nil {
		if strings.TrimSpace(text) == "" {
			return "Command completed successfully.", nil
		}
		return text, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return reportExit(exitError, text), nil
	}
	if state != nil && isWaitDelayError(err) {
		if state.Success() {
			if strings.TrimSpace(text) == "" {
				return "Command completed successfully.", nil
			}
			return text, nil
		}
		return reportExit(&exec.ExitError{ProcessState: state}, text), nil
	}
	return text, fmt.Errorf("command failed: %w", err)
}

func isWaitDelayError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "WaitDelay")
}

func terminateJobWhenProcessExits(job windows.Handle, pid int) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
	_ = windows.TerminateJobObject(job, 1)
}

func lookPowerShell() (string, error) {
	if path, err := exec.LookPath("pwsh"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("powershell"); err == nil {
		return path, nil
	}
	return "", errors.New("PowerShell is not available on PATH")
}

func newKillJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func assignPIDToJob(job windows.Handle, pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.AssignProcessToJobObject(job, handle)
}

func signaledExitLabel(*exec.ExitError) string {
	return ""
}
