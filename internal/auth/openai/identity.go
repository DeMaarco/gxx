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

// Codex OAuth client parameters. Isolated here so a client-id or endpoint
// change stays in one file. This is not a public third-party product API.
const (
	ClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	Issuer       = "https://auth.openai.com"
	AuthorizeURL = Issuer + "/oauth/authorize"
	TokenURL     = Issuer + "/oauth/token"
	Scope        = "openid profile email offline_access"
	Originator   = "gxx"

	deviceUserCodePath = "/api/accounts/deviceauth/usercode"
	devicePollPath     = "/api/accounts/deviceauth/token"
	deviceCallbackPath = "/deviceauth/callback"
	deviceVerifyPath   = "/codex/device"

	callbackHost = "127.0.0.1"
	callbackPath = "/auth/callback"
)

var callbackPorts = []int{1455, 1457}
