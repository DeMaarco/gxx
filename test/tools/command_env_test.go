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

package tools_test

import (
	"path/filepath"
	"strings"
	"testing"

	"gxx/internal/tools"
)

func TestSanitizedEnvironmentInjectsGoCachesFromHome(t *testing.T) {
	env := tools.SanitizedEnvironment([]string{
		"HOME=/tmp/gxx-home-should-not-leak",
		"PATH=/usr/bin",
		"OPENAI_API_KEY=secret",
		"ANTHROPIC_API_KEY=ant-secret",
		"ANTHROPIC_AUTH_TOKEN=ant-token",
		"CLAUDE_CODE_OAUTH_TOKEN=oauth-secret",
	})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "HOME=/tmp/gxx-home-should-not-leak") {
		t.Fatalf("HOME missing: %q", env)
	}
	for _, leaked := range []string{"OPENAI_API_KEY=", "ANTHROPIC_API_KEY=", "ANTHROPIC_AUTH_TOKEN=", "CLAUDE_CODE_OAUTH_TOKEN="} {
		if strings.Contains(joined, leaked) {
			t.Fatalf("credential leaked (%s): %q", leaked, env)
		}
	}
	if !strings.Contains(joined, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("GIT_TERMINAL_PROMPT missing: %q", env)
	}
	wantMod := "GOMODCACHE=" + filepath.Join("/tmp/gxx-home-should-not-leak", "go", "pkg", "mod")
	wantCache := "GOCACHE=" + tools.DefaultGoCache("/tmp/gxx-home-should-not-leak")
	if !strings.Contains(joined, wantMod) || !strings.Contains(joined, wantCache) {
		t.Fatalf("env = %q, want %s and %s", env, wantMod, wantCache)
	}
}

func TestSanitizedEnvironmentKeepsExistingGoCaches(t *testing.T) {
	env := tools.SanitizedEnvironment([]string{
		"HOME=/tmp/gxx-home-should-not-leak",
		"GOMODCACHE=/custom/mod",
		"GOCACHE=/custom/cache",
	})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GOMODCACHE=/custom/mod") || !strings.Contains(joined, "GOCACHE=/custom/cache") {
		t.Fatalf("existing Go caches were replaced: %q", env)
	}
	if strings.Count(joined, "GOMODCACHE=") != 1 || strings.Count(joined, "GOCACHE=") != 1 {
		t.Fatalf("duplicate Go cache entries: %q", env)
	}
}

func TestSanitizedEnvironmentStripsInjectionVariables(t *testing.T) {
	env := tools.SanitizedEnvironment([]string{
		"PATH=/usr/bin",
		"NODE_OPTIONS=--require ./evil.js",
		"PYTHONSTARTUP=/tmp/evil.py",
		"LD_PRELOAD=/tmp/evil.so",
		"IFS=/",
		"HOME=/tmp/gxx-home",
	})
	joined := strings.Join(env, "\n")
	for _, leaked := range []string{"NODE_OPTIONS=", "PYTHONSTARTUP=", "LD_PRELOAD=", "IFS="} {
		if strings.Contains(joined, leaked) {
			t.Fatalf("injection variable leaked (%s): %q", leaked, env)
		}
	}
}

func TestSanitizedEnvironmentUsesUserProfileWhenHomeMissing(t *testing.T) {
	home := filepath.FromSlash("C:/Users/gxx-test")
	env := tools.SanitizedEnvironment([]string{
		"USERPROFILE=" + home,
		"PATH=C:\\Windows",
	})
	joined := strings.Join(env, "\n")
	wantMod := "GOMODCACHE=" + filepath.Join(home, "go", "pkg", "mod")
	wantCache := "GOCACHE=" + tools.DefaultGoCache(home)
	if !strings.Contains(joined, wantMod) || !strings.Contains(joined, wantCache) {
		t.Fatalf("env = %q, want %s and %s", env, wantMod, wantCache)
	}
}
