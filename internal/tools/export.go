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

const (
	MaxToolCallsPerBatch  = maxToolCallsPerBatch
	DefaultIgnorePatterns = defaultIgnorePatterns
)

var (
	CompactDiff          = compactDiff
	SanitizedEnvironment = sanitizedEnvironment
	DefaultGoCache       = defaultGoCache
)

type IgnoreMatcher = ignoreMatcher

func NewIgnoreMatcher() *IgnoreMatcher {
	return &ignoreMatcher{}
}

func (m *ignoreMatcher) AddFile(dir, contents string) {
	m.addFile(dir, contents)
}

func (m *ignoreMatcher) Ignores(path string, isDir bool) bool {
	return m.ignores(path, isDir)
}
