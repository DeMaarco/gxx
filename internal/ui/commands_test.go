package ui

import (
	"strings"
	"testing"
)

func TestLookupSlashCommand(t *testing.T) {
	tests := []struct {
		line    string
		name    string
		wantErr string
	}{
		{line: "/help", name: "/help"},
		{line: "/quit", name: "/exit"},
		{line: "/model terra", name: "/model"},
		{line: "/mode auto", name: "/mode"},
		{line: "/foo", wantErr: "unknown command /foo"},
		{line: "/help extra", wantErr: "unexpected argument for /help"},
		{line: "/clear now", wantErr: "unexpected argument for /clear"},
		{line: "/modelxyz", wantErr: "unknown command /modelxyz"},
	}
	for _, test := range tests {
		name, _, err := lookupSlashCommand(test.line)
		if test.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("lookupSlashCommand(%q) error = %v, want %q", test.line, err, test.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("lookupSlashCommand(%q) error = %v", test.line, err)
		}
		if name != test.name {
			t.Fatalf("lookupSlashCommand(%q) name = %q, want %q", test.line, name, test.name)
		}
	}
}
