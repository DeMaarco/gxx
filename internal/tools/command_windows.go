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

	output := &limitedWriter{limit: r.maxResultBytes * 2}
	command, err := startPowerShell(shell, r.workspace.Root(), args.Command, output)
	if err != nil {
		return "", err
	}

	job, err := newKillJob()
	if err != nil {
		_ = command.Process.Kill()
		return "", fmt.Errorf("create process job: %w", err)
	}
	defer windows.CloseHandle(job)

	if err := assignAndResume(job, command.Process); err != nil {
		killWindowsCommand(job, command.Process.Pid)
		_ = command.Process.Kill()
		return "", err
	}

	// Kill leftover children as soon as PowerShell exits so they cannot
	// hold stdout/stderr open and stall exec.Cmd.Wait (WaitDelay).
	go terminateJobWhenProcessExits(job, command.Process.Pid)

	waited := make(chan error, 1)
	go func() {
		waited <- command.Wait()
	}()

	select {
	case err := <-waited:
		killWindowsCommand(job, command.Process.Pid)
		text, runErr := finishWindowsCommand(output.String(), err, command.ProcessState)
		if runErr != nil {
			return text, runErr
		}
		return withMissingCommandHint(args.Command, text), nil
	case <-commandContext.Done():
		killWindowsCommand(job, command.Process.Pid)
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

func startPowerShell(shell, dir, script string, output *limitedWriter) (*exec.Cmd, error) {
	env := sanitizedEnvironment(os.Environ())
	command, err := startPowerShellWithFlags(shell, dir, script, env, output, windows.CREATE_BREAKAWAY_FROM_JOB)
	if err == nil {
		return command, nil
	}
	command, err = startPowerShellWithFlags(shell, dir, script, env, output, 0)
	if err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}
	return command, nil
}

func startPowerShellWithFlags(
	shell, dir, script string,
	env []string,
	output *limitedWriter,
	extraFlags uint32,
) (*exec.Cmd, error) {
	command := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	command.Dir = dir
	command.Env = env
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		// Suspend until the process is in the kill-on-close job so children
		// cannot start outside it. Break away from a parent job when allowed
		// so grandchildren inherit this job instead of a nested outer one.
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED | extraFlags,
	}
	command.WaitDelay = 2 * time.Second
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command, nil
}

func terminateJobWhenProcessExits(job windows.Handle, pid int) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
	killWindowsCommand(job, pid)
}

func killWindowsCommand(job windows.Handle, pid int) {
	children := processDescendants(pid)
	_ = windows.TerminateJobObject(job, 1)
	for _, child := range children {
		terminatePID(child)
	}
	terminatePID(pid)
}

func terminatePID(pid int) {
	if pid <= 0 {
		return
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	_ = windows.TerminateProcess(handle, 1)
}

func processDescendants(root int) []int {
	if root <= 0 {
		return nil
	}
	parents, err := processParents()
	if err != nil {
		return nil
	}
	rootID := uint32(root)
	var children []int
	for pid := range parents {
		if pid == rootID {
			continue
		}
		if hasAncestor(parents, pid, rootID) {
			children = append(children, int(pid))
		}
	}
	return children
}

func processParents() (map[uint32]uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	parents := make(map[uint32]uint32)
	for {
		parents[entry.ProcessID] = entry.ParentProcessID
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return parents, nil
}

func hasAncestor(parents map[uint32]uint32, pid, ancestor uint32) bool {
	seen := map[uint32]bool{}
	for pid != 0 && !seen[pid] {
		if pid == ancestor {
			return true
		}
		seen[pid] = true
		next, ok := parents[pid]
		if !ok || next == pid {
			return false
		}
		pid = next
	}
	return false
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

func assignAndResume(job windows.Handle, process *os.Process) error {
	if process == nil {
		return errors.New("command process is nil")
	}
	if err := assignPIDToJob(job, process.Pid); err != nil {
		return fmt.Errorf("confine command process: %w", err)
	}
	if err := resumeProcess(process.Pid); err != nil {
		return fmt.Errorf("resume command process: %w", err)
	}
	return nil
}

func assignPIDToJob(job windows.Handle, pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.AssignProcessToJobObject(job, handle)
}

var ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

func resumeProcess(pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SUSPEND_RESUME, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	status, _, _ := ntResumeProcess.Call(uintptr(handle))
	if status != 0 {
		return fmt.Errorf("NtResumeProcess status 0x%x", status)
	}
	return nil
}

func signaledExitLabel(*exec.ExitError) string {
	return ""
}
