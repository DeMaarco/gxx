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

package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://api.anthropic.com"

type modelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	HasMore bool   `json:"has_more"`
	LastID  string `json:"last_id"`
}

// ListModels returns Claude model IDs available to the OAuth token.
func ListModels(ctx context.Context, client *http.Client, baseURL, token string) ([]string, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultAPIBaseURL
	}
	ids := make([]string, 0, 32)
	after := ""
	for page := 0; page < 8; page++ {
		pageIDs, next, more, err := listModelPage(ctx, client, baseURL, token, after)
		if err != nil {
			return nil, err
		}
		ids = append(ids, pageIDs...)
		if !more || next == "" || next == after {
			break
		}
		after = next
	}
	return ids, nil
}

func listModelPage(ctx context.Context, client *http.Client, baseURL, token, after string) ([]string, string, bool, error) {
	endpoint, err := url.Parse(strings.TrimRight(baseURL, "/") + "/v1/models")
	if err != nil {
		return nil, "", false, err
	}
	query := endpoint.Query()
	query.Set("limit", "100")
	if after != "" {
		query.Set("after_id", after)
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", oauthBetaHeader)
	req.Header.Set("x-app", oauthAppHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gxx")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", false, err
	}
	if resp.StatusCode >= 400 {
		return nil, "", false, fmt.Errorf("list models: HTTP %d: %s", resp.StatusCode, compactErrorText(body))
	}
	var page modelList
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", false, err
	}
	ids := make([]string, 0, len(page.Data))
	for _, item := range page.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, strings.TrimSpace(page.LastID), page.HasMore, nil
}

func compactErrorText(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 180 {
		return text[:180] + "…"
	}
	if text == "" {
		return "request failed"
	}
	return text
}
