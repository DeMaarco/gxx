package approval

import (
	"context"
	"strings"
	"testing"

	"gxx/internal/config"
)

func TestPolicyAskDefersToInner(t *testing.T) {
	inner := &recordingApprover{approved: false}
	policy := NewPolicy(config.PermissionAsk, inner)
	approved, err := policy.Approve(context.Background(), Action{
		Title: "Edit file",
		Kind:  KindWrite,
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if approved {
		t.Fatal("ask mode approved a write without the inner approver")
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}
}

func TestPolicyAutoWritesSkipsWritePrompts(t *testing.T) {
	inner := &recordingApprover{approved: false}
	policy := NewPolicy(config.PermissionAutoWrites, inner)
	approved, err := policy.Approve(context.Background(), Action{
		Title:   "Write file",
		Preview: "+content",
		Kind:    KindWrite,
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if !approved {
		t.Fatal("auto-writes should approve file changes")
	}
	if inner.calls != 0 {
		t.Fatalf("inner calls = %d, want 0", inner.calls)
	}

	approved, err = policy.Approve(context.Background(), Action{
		Title:   "Run command",
		Preview: "$ ls",
		Kind:    KindCommand,
	})
	if err != nil {
		t.Fatalf("Approve() command error = %v", err)
	}
	if approved {
		t.Fatal("auto-writes should still ask for commands")
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}
}

func TestPolicyAutoApprovesWritesAndCommands(t *testing.T) {
	inner := &recordingApprover{approved: false}
	policy := NewPolicy("yolo", inner)
	if policy.Mode() != config.PermissionAuto {
		t.Fatalf("Mode() = %q, want auto", policy.Mode())
	}
	for _, action := range []Action{
		{Title: "Write file", Kind: KindWrite},
		{Title: "Run command", Kind: KindCommand},
	} {
		approved, err := policy.Approve(context.Background(), action)
		if err != nil {
			t.Fatalf("Approve(%s) error = %v", action.Title, err)
		}
		if !approved {
			t.Fatalf("auto mode denied %s", action.Title)
		}
	}
	if inner.calls != 0 {
		t.Fatalf("inner calls = %d, want 0", inner.calls)
	}
}

func TestPolicyAutoApprovesOversizedPreview(t *testing.T) {
	policy := NewPolicy(config.PermissionAuto, &recordingApprover{approved: false})
	approved, err := policy.Approve(context.Background(), Action{
		Title:   "Write",
		Preview: strings.Repeat("x", maxPreviewBytes+1),
		Kind:    KindWrite,
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if !approved {
		t.Fatal("auto mode denied an oversized write preview")
	}
}

func TestPolicySetModeRejectsUnknownValues(t *testing.T) {
	policy := NewPolicy(config.PermissionAsk, nil)
	if err := policy.SetMode("trust-me"); err == nil {
		t.Fatal("SetMode(trust-me) succeeded")
	}
	if policy.Mode() != config.PermissionAsk {
		t.Fatalf("Mode() = %q after rejected set", policy.Mode())
	}
}

type recordingApprover struct {
	approved bool
	calls    int
}

func (a *recordingApprover) Approve(context.Context, Action) (bool, error) {
	a.calls++
	return a.approved, nil
}
