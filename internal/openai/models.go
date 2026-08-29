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
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type modelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// ListModels returns model IDs from the platform or Codex /models endpoint.
func ListModels(ctx context.Context, client *http.Client, baseURL, token string) ([]string, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultAPIBaseURL
	}
	header, body, err := doUsageRequest(ctx, client, baseURL, "/models", token, nil, nil)
	_ = header
	if err != nil {
		return nil, err
	}
	var page modelList
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(page.Data))
	for _, item := range page.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
