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
	"context"
	"strings"
	"testing"

	"gxx/internal/approval"

	"gxx/internal/config"
)

func TestPolicyAskDefersToInner(t *testing.T) {
	inner := &recordingApprover{approved: false}
	policy := approval.NewPolicy(config.PermissionAsk, inner)
	decision, err := policy.Approve(context.Background(), approval.Action{
		Title: "Edit file",
		Kind:  approval.KindWrite,
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if decision.Approved {
		t.Fatal("ask mode approved a write without the inner approver")
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}
}

func TestPolicyAutoWritesSkipsWritePrompts(t *testing.T) {
	inner := &recordingApprover{approved: false}
	policy := approval.NewPolicy(config.PermissionAutoWrites, inner)
	decision, err := policy.Approve(context.Background(), approval.Action{
		Title:   "Write file",
		Preview: "+content",
		Kind:    approval.KindWrite,
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if !decision.Approved {
		t.Fatal("auto-writes should approve file changes")
	}
	if inner.calls != 0 {
		t.Fatalf("inner calls = %d, want 0", inner.calls)
	}

	decision, err = policy.Approve(context.Background(), approval.Action{
		Title:   "Run command",
		Preview: "$ ls",
		Kind:    approval.KindCommand,
	})
	if err != nil {
		t.Fatalf("Approve() command error = %v", err)
	}
	if decision.Approved {
		t.Fatal("auto-writes should still ask for commands")
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}
}

func TestPolicyAutoApprovesWritesAndCommands(t *testing.T) {
	inner := &recordingApprover{approved: false}
	policy := approval.NewPolicy("yolo", inner)
	if policy.Mode() != config.PermissionAuto {
		t.Fatalf("Mode() = %q, want auto", policy.Mode())
	}
	for _, action := range []approval.Action{
		{Title: "Write file", Kind: approval.KindWrite},
		{Title: "Run command", Kind: approval.KindCommand},
	} {
		decision, err := policy.Approve(context.Background(), action)
		if err != nil {
			t.Fatalf("Approve(%s) error = %v", action.Title, err)
		}
		if !decision.Approved {
			t.Fatalf("auto mode denied %s", action.Title)
		}
	}
	if inner.calls != 0 {
		t.Fatalf("inner calls = %d, want 0", inner.calls)
	}
}

func TestPolicyAutoApprovesOversizedPreview(t *testing.T) {
	policy := approval.NewPolicy(config.PermissionAuto, &recordingApprover{approved: false})
	decision, err := policy.Approve(context.Background(), approval.Action{
		Title:   "Write",
		Preview: strings.Repeat("x", approval.MaxPreviewBytes+1),
		Kind:    approval.KindWrite,
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if !decision.Approved {
		t.Fatal("auto mode denied an oversized write preview")
	}
}

func TestPolicySetModeRejectsUnknownValues(t *testing.T) {
	policy := approval.NewPolicy(config.PermissionAsk, nil)
	if err := policy.SetMode("trust-me"); err == nil {
		t.Fatal("SetMode(trust-me) succeeded")
	}
	if policy.Mode() != config.PermissionAsk {
		t.Fatalf("Mode() = %q after rejected set", policy.Mode())
	}
}

func TestPolicyRemembersExactCommandForSession(t *testing.T) {
	inner := &recordingApprover{approved: true, remember: true}
	policy := approval.NewPolicy(config.PermissionAsk, inner)
	action := approval.Action{
		Title:     "Run command",
		Kind:      approval.KindCommand,
		RepeatKey: "go test ./...",
	}
	decision, err := policy.Approve(context.Background(), action)
	if err != nil {
		t.Fatalf("first Approve() error = %v", err)
	}
	if !decision.Approved || !decision.Remember {
		t.Fatalf("first decision = %+v, want remember", decision)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}

	inner.approved = false
	inner.remember = false
	decision, err = policy.Approve(context.Background(), action)
	if err != nil {
		t.Fatalf("second Approve() error = %v", err)
	}
	if !decision.Approved {
		t.Fatal("remembered command was not auto-approved")
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1 after remember", inner.calls)
	}

	other, err := policy.Approve(context.Background(), approval.Action{
		Title:     "Run command",
		Kind:      approval.KindCommand,
		RepeatKey: "go test ./internal/...",
	})
	if err != nil {
		t.Fatalf("other Approve() error = %v", err)
	}
	if other.Approved {
		t.Fatal("a different command should still ask")
	}
	if inner.calls != 2 {
		t.Fatalf("inner calls = %d, want 2 for a new command", inner.calls)
	}
}

func TestPolicySetModeClearsRememberedCommands(t *testing.T) {
	inner := &recordingApprover{approved: true, remember: true}
	policy := approval.NewPolicy(config.PermissionAsk, inner)
	action := approval.Action{
		Kind:      approval.KindCommand,
		RepeatKey: "go test ./...",
	}
	if _, err := policy.Approve(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	if err := policy.SetMode(config.PermissionAsk); err != nil {
		t.Fatal(err)
	}
	inner.approved = false
	inner.remember = false
	decision, err := policy.Approve(context.Background(), action)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Approved {
		t.Fatal("SetMode should clear the session allowlist")
	}
	if inner.calls != 2 {
		t.Fatalf("inner calls = %d, want 2 after SetMode", inner.calls)
	}
}

func TestPolicyDoesNotRememberWrites(t *testing.T) {
	inner := &recordingApprover{approved: true, remember: true}
	policy := approval.NewPolicy(config.PermissionAsk, inner)
	decision, err := policy.Approve(context.Background(), approval.Action{
		Title: "Write file",
		Kind:  approval.KindWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Approved {
		t.Fatal("write should be approved by inner")
	}
	inner.approved = false
	second, err := policy.Approve(context.Background(), approval.Action{
		Title: "Write file",
		Kind:  approval.KindWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Approved {
		t.Fatal("writes must not be remembered without RepeatKey")
	}
	if inner.calls != 2 {
		t.Fatalf("inner calls = %d, want 2", inner.calls)
	}
}

type recordingApprover struct {
	approved bool
	remember bool
	calls    int
}

func (a *recordingApprover) Approve(context.Context, approval.Action) (approval.Decision, error) {
	a.calls++
	return approval.Decision{Approved: a.approved, Remember: a.remember && a.approved}, nil
}
