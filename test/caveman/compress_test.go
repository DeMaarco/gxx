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

package caveman_test

import (
	"strings"
	"testing"

	"gxx/internal/caveman"
)

func TestCompressLiteKeepsArticles(t *testing.T) {
	got := caveman.Compress("Please just read the file.", caveman.Lite)
	if strings.Contains(got, "Please") || strings.Contains(got, "just") {
		t.Fatalf("lite kept filler: %q", got)
	}
	if !strings.Contains(got, "the file") {
		t.Fatalf("lite dropped articles: %q", got)
	}
}

func TestCompressFullDropsArticles(t *testing.T) {
	got := caveman.Compress("Read the file and list the results.", caveman.Full)
	if strings.Contains(got, " the ") {
		t.Fatalf("full kept articles: %q", got)
	}
	if !strings.Contains(got, "file") || !strings.Contains(got, "results") {
		t.Fatalf("full dropped substance: %q", got)
	}
}

func TestCompressKeepsCodePathsAndURLs(t *testing.T) {
	input := "Please check `apply_patch` in internal/tools/fs.go and https://example.com/docs."
	got := caveman.Compress(input, caveman.Full)
	if !strings.Contains(got, "`apply_patch`") {
		t.Fatalf("lost inline code: %q", got)
	}
	if !strings.Contains(got, "internal/tools/fs.go") {
		t.Fatalf("lost path: %q", got)
	}
	if !strings.Contains(got, "https://example.com/docs.") && !strings.Contains(got, "https://example.com/docs") {
		t.Fatalf("lost url: %q", got)
	}
}

func TestCompressKeepsNegation(t *testing.T) {
	got := caveman.Compress("Do not delete the file.", caveman.Full)
	if !strings.Contains(got, "not") {
		t.Fatalf("dropped negation: %q", got)
	}
}

func TestCompressDescriptionsRewritesNestedFields(t *testing.T) {
	got := caveman.CompressDescriptions(map[string]any{
		"description": "Please list the files in the workspace.",
		"properties": map[string]any{
			"path": map[string]any{"description": "The workspace-relative path."},
		},
	}, caveman.Full).(map[string]any)
	desc, _ := got["description"].(string)
	if strings.Contains(desc, "Please") || strings.Contains(desc, " the ") {
		t.Fatalf("top description = %q", desc)
	}
	props := got["properties"].(map[string]any)
	path := props["path"].(map[string]any)
	nested, _ := path["description"].(string)
	if strings.Contains(nested, "The ") {
		t.Fatalf("nested description = %q", nested)
	}
}
