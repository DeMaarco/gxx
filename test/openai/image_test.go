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

package openai_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gxx/internal/openai"
)

func TestGenerateImageDecodesGPTImageResponse(t *testing.T) {
	const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	want, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s", request.Method)
		}
		if !strings.HasSuffix(request.URL.Path, "/images/generations") {
			t.Errorf("path = %s", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"created":       1,
			"data":          []any{map[string]any{"b64_json": pngB64}},
			"output_format": "png",
			"quality":       "high",
			"size":          "1024x1024",
		})
	}))
	defer server.Close()

	result, err := openai.GenerateImage(context.Background(), "test-key", openai.ImageRequest{
		Prompt:  "a tiny red pixel",
		Model:   "gpt-image-2",
		Size:    "1024x1024",
		Quality: "high",
		Format:  "png",
		BaseURL: server.URL + "/v1",
	})
	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}
	if string(result.Data) != string(want) {
		t.Fatalf("decoded %d bytes, want %d-byte PNG", len(result.Data), len(want))
	}
	if result.Model != "gpt-image-2" || result.Size != "1024x1024" || result.Quality != "high" || result.Format != "png" {
		t.Fatalf("result = %+v", result)
	}
	if body["prompt"] != "a tiny red pixel" || body["model"] != "gpt-image-2" {
		t.Fatalf("request body = %#v", body)
	}
}

func TestGenerateImageRequiresAPIKeyAndPrompt(t *testing.T) {
	_, err := openai.GenerateImage(context.Background(), "", openai.ImageRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("empty key error = %v", err)
	}
	_, err = openai.GenerateImage(context.Background(), "test-key", openai.ImageRequest{})
	if err == nil || !strings.Contains(err.Error(), "prompt cannot be empty") {
		t.Fatalf("empty prompt error = %v", err)
	}
}

func TestGenerateImageSurfacesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"billing hard limit reached","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	_, err := openai.GenerateImage(context.Background(), "test-key", openai.ImageRequest{
		Prompt:  "nope",
		BaseURL: server.URL + "/v1",
	})
	if err == nil || !strings.Contains(err.Error(), "billing hard limit reached") {
		t.Fatalf("error = %v, want API message", err)
	}
}
