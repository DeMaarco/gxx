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

package compact

import (
	"strings"

	"gxx/internal/budget"
)

const (
	// MaxTranscriptBytes caps the dropped-history text sent to the summarizer.
	MaxTranscriptBytes = 32 * 1024
	// MaxSummaryTokens is the output budget for the one-shot summary call.
	MaxSummaryTokens = 1500

	systemPrompt = `You summarize a coding-agent conversation so work can continue after older turns are dropped.
Write a compact structured summary in plain text. Use these headings when relevant:
Goal, Decisions, Files read, Files changed, Errors, Pending.
Keep paths exact. Do not invent facts. Do not call tools.`
)

// BuildPrompt wraps a clipped transcript for the summarizer model.
func BuildPrompt(transcript, focus string) string {
	var b strings.Builder
	b.WriteString("Summarize the following coding-agent transcript.\n")
	if focus = strings.TrimSpace(focus); focus != "" {
		b.WriteString("Prioritize details related to: ")
		b.WriteString(focus)
		b.WriteString("\n")
	}
	b.WriteString("\n<<<TRANSCRIPT\n")
	b.WriteString(budget.ClipBytes(strings.TrimSpace(transcript), MaxTranscriptBytes))
	b.WriteString("\n>>>END TRANSCRIPT\n")
	return b.String()
}

// SystemPrompt is the fixed instruction prefix for summary calls.
func SystemPrompt() string {
	return systemPrompt
}

// NoticeWithSummary prefixes the durable compact notice with a model summary.
func NoticeWithSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return budget.CompactNotice
	}
	return budget.CompactNotice + "\n" + summary
}
