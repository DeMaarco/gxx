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

package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3/option"
)

// Top-level fields chatgpt.com/backend-api/codex/responses accepts.
// Anything else is rejected with HTTP 400 and an empty/non-OpenAI body.
var codexResponseFields = map[string]struct{}{
	"model":               {},
	"instructions":        {},
	"input":               {},
	"tools":               {},
	"tool_choice":         {},
	"parallel_tool_calls": {},
	"reasoning":           {},
	"store":               {},
	"stream":              {},
	"include":             {},
	"service_tier":        {},
	"prompt_cache_key":    {},
	"text":                {},
}

func sanitizeCodexPayload(raw []byte) []byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return raw
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return raw
	}
	for key := range payload {
		if _, ok := codexResponseFields[key]; !ok {
			delete(payload, key)
		}
	}
	if tier, ok := payload["service_tier"].(string); ok {
		switch strings.TrimSpace(tier) {
		case "default", "auto":
		default:
			delete(payload, "service_tier")
		}
	}
	if tools, ok := payload["tools"].([]any); ok {
		for _, tool := range tools {
			object, ok := tool.(map[string]any)
			if !ok {
				continue
			}
			delete(object, "strict")
		}
	}
	sanitized, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return sanitized
}

func rewriteCodexErrorBody(raw []byte) []byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return raw
	}
	if message := parseAPIError(raw); message != "" && message != "request failed" {
		rewritten, err := json.Marshal(map[string]any{
			"error": map[string]string{
				"message": message,
				"type":    "invalid_request_error",
			},
		})
		if err == nil {
			return rewritten
		}
	}
	return raw
}

func sanitizeCodexRequest(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	if req != nil && req.Body != nil && req.Method != http.MethodGet {
		raw, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err == nil {
			sanitized := sanitizeCodexPayload(raw)
			req.Body = io.NopCloser(bytes.NewReader(sanitized))
			req.ContentLength = int64(len(sanitized))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(sanitized)), nil
			}
			var payload map[string]any
			if json.Unmarshal(sanitized, &payload) == nil {
				model, _ := payload["model"].(string)
				if usesResponsesLite(model) {
					req.Header.Set(codexResponsesLiteHeader, "true")
				} else {
					req.Header.Del(codexResponsesLiteHeader)
				}
			}
		}
	}
	resp, err := next(req)
	if err != nil || resp == nil || resp.StatusCode < 400 || resp.Body == nil {
		return resp, err
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return resp, err
	}
	rewritten := rewriteCodexErrorBody(raw)
	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	return resp, nil
}
