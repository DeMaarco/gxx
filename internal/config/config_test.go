package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAndValidate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("GXX_MODEL", "test-model")
	t.Setenv("GXX_MAX_STEPS", "7")
	t.Setenv("GXX_COMMAND_TIMEOUT", "3s")

	settings := Load(t.TempDir())
	if err := settings.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if settings.Model != "test-model" {
		t.Fatalf("Model = %q, want test-model", settings.Model)
	}
	if settings.MaxSteps != 7 {
		t.Fatalf("MaxSteps = %d, want 7", settings.MaxSteps)
	}
	if settings.Effort != DefaultEffort {
		t.Fatalf("Effort = %q, want %s", settings.Effort, DefaultEffort)
	}
	if settings.Context != DefaultContext {
		t.Fatalf("Context = %q, want %s", settings.Context, DefaultContext)
	}
	if settings.Fast {
		t.Fatal("Fast = true, want false")
	}
	if settings.PermissionMode != DefaultPermissionMode {
		t.Fatalf("PermissionMode = %q, want %s", settings.PermissionMode, DefaultPermissionMode)
	}
	if settings.CommandTimeout != 3*time.Second {
		t.Fatalf("CommandTimeout = %s, want 3s", settings.CommandTimeout)
	}
}

func TestValidateRejectsMissingAPIKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings := Load(t.TempDir())
	settings.APIKey = ""

	err := settings.Validate()
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("Validate() error = %v, want missing API key", err)
	}
}

func TestInvalidEnvironmentValuesFallBack(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GXX_MAX_STEPS", "not-a-number")
	t.Setenv("GXX_API_TIMEOUT", "not-a-duration")

	settings := Load(t.TempDir())
	if settings.MaxSteps != DefaultMaxSteps {
		t.Fatalf("MaxSteps = %d, want %d", settings.MaxSteps, DefaultMaxSteps)
	}
	if settings.APITimeout != DefaultAPITimeout {
		t.Fatalf("APITimeout = %s, want %s", settings.APITimeout, DefaultAPITimeout)
	}
}

func TestValidateRejectsUnknownEffort(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	settings := Load(t.TempDir())
	settings.Effort = "ludicrous"
	err := settings.Validate()
	if err == nil || !strings.Contains(err.Error(), "effort") {
		t.Fatalf("Validate() error = %v, want effort error", err)
	}
}

func TestLoadReadsEffortFromEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("GXX_EFFORT", "high")
	settings := Load(t.TempDir())
	if err := settings.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if settings.Effort != "high" {
		t.Fatalf("Effort = %q, want high", settings.Effort)
	}
}

func TestValidateCanonicalizesModelAliases(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	settings := Load(t.TempDir())
	settings.Model = "terra"
	if err := settings.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if settings.Model != "gpt-5.6-terra" {
		t.Fatalf("Model = %q, want gpt-5.6-terra", settings.Model)
	}
}

func TestLoadReadsContextAndFastFromEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("GXX_CONTEXT", "1m")
	t.Setenv("GXX_FAST", "on")
	settings := Load(t.TempDir())
	if err := settings.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if settings.Context != "1m" {
		t.Fatalf("Context = %q, want 1m", settings.Context)
	}
	if !settings.Fast {
		t.Fatal("Fast = false, want true")
	}
}

func TestValidateRejectsUnknownContext(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	settings := Load(t.TempDir())
	settings.Context = "forever"
	err := settings.Validate()
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("Validate() error = %v, want context error", err)
	}
}

func TestCanonicalPermissionAliases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "ask", want: PermissionAsk},
		{input: "auto-writes", want: PermissionAutoWrites},
		{input: "auto-write", want: PermissionAutoWrites},
		{input: "auto", want: PermissionAuto},
		{input: "YOLO", want: PermissionAuto},
	}
	for _, test := range tests {
		got, err := CanonicalPermission(test.input)
		if err != nil {
			t.Fatalf("CanonicalPermission(%q) error = %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("CanonicalPermission(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := CanonicalPermission("trust-me"); err == nil {
		t.Fatal("CanonicalPermission(trust-me) succeeded")
	}
}

func TestLoadReadsPermissionFromEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("GXX_PERMISSION", "yolo")
	settings := Load(t.TempDir())
	if err := settings.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if settings.PermissionMode != PermissionAuto {
		t.Fatalf("PermissionMode = %q, want auto", settings.PermissionMode)
	}
}

func TestValidateRejectsUnknownPermission(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	settings := Load(t.TempDir())
	settings.PermissionMode = "trust-me"
	err := settings.Validate()
	if err == nil || !strings.Contains(err.Error(), "permission mode") {
		t.Fatalf("Validate() error = %v, want permission mode error", err)
	}
}

func TestValidateInteractiveAllowsMissingAPIKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings := Load(t.TempDir())
	settings.APIKey = ""
	if err := settings.ValidateInteractive(); err != nil {
		t.Fatalf("ValidateInteractive() error = %v", err)
	}
}

func TestSaveAndLoadAPIKeyWithPrivatePermissions(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("OPENAI_API_KEY", "")

	path, err := SaveAPIKey("  test-persisted-key  ")
	if err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}
	wantPath := filepath.Join(base, "gxx", "config.json")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", info.Mode().Perm())
	}
	key, err := LoadAPIKey()
	if err != nil {
		t.Fatalf("LoadAPIKey() error = %v", err)
	}
	if key != "test-persisted-key" {
		t.Fatalf("key = %q", key)
	}
	settings := Load(t.TempDir())
	if settings.APIKey != "test-persisted-key" {
		t.Fatalf("loaded config key = %q", settings.APIKey)
	}
}

func TestEnvironmentAPIKeyOverridesPersistentConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := SaveAPIKey("persisted"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "environment")

	settings := Load(t.TempDir())
	if settings.APIKey != "environment" {
		t.Fatalf("APIKey = %q, want environment", settings.APIKey)
	}
}

func TestSaveSessionPersistsAndLoads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GXX_MODEL", "")
	t.Setenv("GXX_EFFORT", "")
	t.Setenv("GXX_CONTEXT", "")
	t.Setenv("GXX_FAST", "")
	t.Setenv("GXX_PERMISSION", "")

	if _, err := SaveAPIKey("persisted-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveSession("gpt-5.6-terra", "high", "1m", true, PermissionAutoWrites); err != nil {
		t.Fatal(err)
	}

	settings := Load(t.TempDir())
	if settings.APIKey != "persisted-key" {
		t.Fatalf("APIKey = %q", settings.APIKey)
	}
	if settings.Model != "gpt-5.6-terra" || settings.Effort != "high" || settings.Context != "1m" {
		t.Fatalf("session = %+v", settings)
	}
	if !settings.Fast || settings.PermissionMode != PermissionAutoWrites {
		t.Fatalf("session = %+v", settings)
	}
}

func TestEnvironmentOverridesPersistedSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := SaveSession("gpt-5.6-luna", "low", "32k", true, PermissionAuto); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GXX_MODEL", "gpt-5.6-sol")
	t.Setenv("GXX_EFFORT", "medium")
	t.Setenv("GXX_CONTEXT", "272k")
	t.Setenv("GXX_FAST", "off")
	t.Setenv("GXX_PERMISSION", "ask")

	settings := Load(t.TempDir())
	if settings.Model != "gpt-5.6-sol" || settings.Effort != "medium" || settings.Context != "272k" {
		t.Fatalf("settings = %+v", settings)
	}
	if settings.Fast || settings.PermissionMode != PermissionAsk {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestSaveAPIKeyPreservesSessionFields(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := SaveSession("gpt-5.6-terra", "high", "128k", true, PermissionAsk); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAPIKey("later-key"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GXX_MODEL", "")
	t.Setenv("GXX_EFFORT", "")
	t.Setenv("GXX_CONTEXT", "")
	t.Setenv("GXX_FAST", "")
	t.Setenv("GXX_PERMISSION", "")

	settings := Load(t.TempDir())
	if settings.APIKey != "later-key" {
		t.Fatalf("APIKey = %q", settings.APIKey)
	}
	if settings.Model != "gpt-5.6-terra" || settings.Effort != "high" || !settings.Fast {
		t.Fatalf("session lost after SaveAPIKey: %+v", settings)
	}
}

func TestLoadIgnoresInvalidPersistedSessionFields(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GXX_MODEL", "")
	t.Setenv("GXX_EFFORT", "")
	t.Setenv("GXX_CONTEXT", "")
	t.Setenv("GXX_PERMISSION", "")
	directory := filepath.Join(base, "gxx")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(`{"effort":"ludicrous","context":"nope","permission":"yolo-ish"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	settings := Load(t.TempDir())
	if settings.Effort != DefaultEffort || settings.Context != DefaultContext || settings.PermissionMode != DefaultPermissionMode {
		t.Fatalf("invalid persisted fields were not ignored: %+v", settings)
	}
}

func TestSaveAPIKeyRejectsSymlinkedConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	directory := filepath.Join(base, "gxx")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "config.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadAPIKey(); err == nil {
		t.Fatal("LoadAPIKey() followed a symlink")
	}
	if _, err := SaveAPIKey("new-key"); err == nil {
		t.Fatal("SaveAPIKey() replaced a symlink")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "unchanged" {
		t.Fatalf("outside file changed: %q", data)
	}
}
