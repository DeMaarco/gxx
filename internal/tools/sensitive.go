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

package tools

import (
	"fmt"
	pathpkg "path"
	"strings"
)

var sensitiveBasenames = map[string]struct{}{
	".netrc":               {},
	".npmrc":               {},
	".pypirc":              {},
	"credentials":          {},
	"credentials.json":     {},
	"id_dsa":               {},
	"id_ecdsa":             {},
	"id_ed25519":           {},
	"id_rsa":               {},
	"kubeconfig":           {},
	"secret.json":          {},
	"secrets.json":         {},
	"service_account.json": {},
}

var sensitiveDirectories = map[string]struct{}{
	".aws":   {},
	".gnupg": {},
	".kube":  {},
	".ssh":   {},
}

var sensitiveSuffixes = []string{
	".jks", ".key", ".keystore", ".p12", ".pem", ".pfx",
}

func isSensitivePath(value string) bool {
	clean := strings.ToLower(pathpkg.Clean(strings.ReplaceAll(value, "\\", "/")))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" || clean == "." {
		return false
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git/") || strings.Contains(clean, "/.git/") {
		return true
	}
	for _, part := range strings.Split(clean, "/") {
		if _, ok := sensitiveDirectories[part]; ok {
			return true
		}
	}
	base := pathpkg.Base(clean)
	if isSensitiveEnvName(base) {
		return true
	}
	if _, ok := sensitiveBasenames[base]; ok {
		return true
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(base, suffix) || strings.Contains(base, suffix+".") {
			return true
		}
	}
	return false
}

func isSensitiveEnvName(base string) bool {
	return base == ".env" ||
		strings.HasPrefix(base, ".env.") ||
		strings.HasSuffix(base, ".env") ||
		strings.Contains(base, ".env.")
}

func refuseSensitive(operation, path string) error {
	return fmt.Errorf("refusing to %s sensitive path: %s", operation, path)
}

func omittedSensitiveNotice(count int) string {
	if count == 1 {
		return "… 1 sensitive path omitted by gxx"
	}
	return fmt.Sprintf("… %d sensitive paths omitted by gxx", count)
}
