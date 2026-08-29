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

package config_test

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"gxx/internal/config"
)

func TestSaveAPIKeySetsOwnerOnlyWindowsACL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := config.SaveAPIKey("acl-secret-key")
	if err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo: %v", err)
	}
	sddl := sd.String()
	if !strings.Contains(sddl, "D:P") && !strings.Contains(sddl, "D:p") {
		t.Fatalf("SDDL = %q, want a protected DACL", sddl)
	}
	upper := strings.ToUpper(sddl)
	if strings.Contains(upper, ";;WD") || strings.Contains(upper, ";;BU") {
		t.Fatalf("SDDL grants Everyone or Users: %q", sddl)
	}

	key, err := config.LoadAPIKey()
	if err != nil {
		t.Fatalf("LoadAPIKey() error = %v", err)
	}
	if key != "acl-secret-key" {
		t.Fatalf("key = %q after ACL lockdown", key)
	}
}
