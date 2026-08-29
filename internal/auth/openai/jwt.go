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
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// ParseAccountID reads chatgpt_account_id from an id_token JWT payload.
// The signature is not verified; the token was just received from OpenAI.
func ParseAccountID(idToken string) (string, error) {
	idToken = strings.TrimSpace(idToken)
	if idToken == "" {
		return "", errors.New("id_token is empty")
	}
	payload, err := decodeJWTPayload(idToken)
	if err != nil {
		return "", err
	}
	var claims struct {
		AccountID string `json:"chatgpt_account_id"`
		Auth      struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	if id := strings.TrimSpace(claims.Auth.AccountID); id != "" {
		return id, nil
	}
	if id := strings.TrimSpace(claims.AccountID); id != "" {
		return id, nil
	}
	return "", errors.New("id_token omitted chatgpt_account_id")
}

func decodeJWTPayload(token string) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("id_token is not a JWT")
	}
	raw := parts[1]
	if data, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return data, nil
	}
	if n := len(raw) % 4; n != 0 {
		raw += strings.Repeat("=", 4-n)
	}
	data, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	return data, nil
}
