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

package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gxx/internal/anthropic"
)

func TestListModelsPaginatesAndSendsOAuthHeaders(t *testing.T) {
	var pages, auth, app, ua int
	var lastAfter string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			http.NotFound(writer, request)
			return
		}
		pages++
		if request.Header.Get("Authorization") == "Bearer tok" {
			auth++
		}
		if request.Header.Get("x-app") == "cli" {
			app++
		}
		if request.Header.Get("User-Agent") == "gxx" {
			ua++
		}
		lastAfter = request.URL.Query().Get("after_id")
		writer.Header().Set("Content-Type", "application/json")
		if lastAfter == "" {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"data":     []any{map[string]any{"id": "claude-sonnet-5"}, map[string]any{"id": "claude-opus-5"}},
				"has_more": true,
				"last_id":  "claude-opus-5",
			})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"data":     []any{map[string]any{"id": "claude-haiku-4-5"}, map[string]any{"id": "claude-fable-5"}},
			"has_more": false,
			"last_id":  "claude-fable-5",
		})
	}))
	defer server.Close()

	ids, err := anthropic.ListModels(context.Background(), server.Client(), server.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || lastAfter != "claude-opus-5" || auth != 2 || app != 2 || ua != 2 {
		t.Fatalf("pages=%d after=%q auth=%d app=%d ua=%d", pages, lastAfter, auth, app, ua)
	}
	want := []string{"claude-sonnet-5", "claude-opus-5", "claude-haiku-4-5", "claude-fable-5"}
	if len(ids) != 4 || ids[0] != want[0] || ids[3] != want[3] {
		t.Fatalf("ids = %#v, want %#v", ids, want)
	}
}
