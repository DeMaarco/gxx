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

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gxx/internal/agent"
)

const (
	maxReviewFindings  = 8
	maxReviewFiles     = 8
	reviewThinkFooter  = "Think through remaining defects before the final answer. Fix findings or say why they are false."
	reviewStaticFooter = "For static HTML/CSS/JS this review is the validation; do not skip it because there are no automated tests."
)

var (
	zIndexNegativeRe = regexp.MustCompile(`(?i)z-index\s*:\s*(-\d+)`)
	htmlHrefHashRe   = regexp.MustCompile(`(?i)href\s*=\s*["']#([^"']+)["']`)
	htmlIDRe         = regexp.MustCompile(`(?i)\bid\s*=\s*["']([^"']+)["']`)
	htmlTitleRe      = regexp.MustCompile(`(?i)<title\b`)
	htmlViewportRe   = regexp.MustCompile(`(?i)<meta\b[^>]*\bname\s*=\s*["']viewport["']`)
)

type reviewFileArgs struct {
	Path string `json:"path"`
}

func (r *Registry) reviewFileSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name: "review_file",
			Description: "Review a workspace text file after writing it. apply_patch already attaches this review for created or updated files. " +
				"Call again after fixes. Read findings, fix them, then think through remaining defects before the final answer. " +
				"This is a static review, not a visual preview.",
			ReadOnly: true,
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative file path.",
				},
			}, "path"),
		},
		run: r.reviewFile,
	}
}

func (r *Registry) reviewFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args reviewFileArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	path := strings.TrimSpace(args.Path)
	if path == "" {
		return "", errors.New("path cannot be empty")
	}
	if isSensitivePath(path) {
		return "", refuseSensitive("review", path)
	}
	clean, err := r.workspace.Clean(path)
	if err != nil {
		return "", err
	}
	data, err := r.workspace.ReadRegularFile(clean, maxEditableFile)
	if err != nil {
		return "", err
	}
	return formatReview(clean, data), nil
}

func formatReview(path string, data []byte) string {
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return fmt.Sprintf("[review_file %s]\nskipped binary file", path)
	}
	content := string(data)
	lines := strings.Count(content, "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		lines++
	}
	findings := reviewFindings(path, content)
	var b strings.Builder
	fmt.Fprintf(&b, "[review_file %s — %d lines]\n", path, lines)
	if len(findings) == 0 {
		b.WriteString("findings: none\n")
	} else {
		b.WriteString("findings:\n")
		for _, finding := range findings {
			b.WriteString("- ")
			b.WriteString(finding)
			b.WriteByte('\n')
		}
	}
	b.WriteString(reviewThinkFooter)
	if looksLikeWebFile(path, content) {
		b.WriteByte('\n')
		b.WriteString(reviewStaticFooter)
	}
	return b.String()
}

func reviewFindings(path, content string) []string {
	if strings.TrimSpace(content) == "" {
		return []string{"file is empty"}
	}
	if !looksLikeWebFile(path, content) {
		return nil
	}
	var findings []string
	findings = appendZIndexFindings(findings, content)
	findings = appendHTMLLinkFindings(findings, content)
	findings = appendHTMLDocumentFindings(findings, path, content)
	if len(findings) > maxReviewFindings {
		omitted := len(findings) - maxReviewFindings
		findings = findings[:maxReviewFindings]
		findings = append(findings, fmt.Sprintf("%d more finding(s) omitted", omitted))
	}
	return findings
}

func looksLikeWebFile(path, content string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm", ".css", ".js", ".svg":
		return true
	}
	lower := strings.ToLower(content)
	return strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<!doctype") ||
		strings.Contains(lower, "<style") ||
		zIndexNegativeRe.MatchString(content)
}

func appendZIndexFindings(findings []string, content string) []string {
	for i, line := range strings.Split(content, "\n") {
		match := zIndexNegativeRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"line %d: negative z-index (%s) can hide the element behind its parent background",
			i+1,
			match[1],
		))
		if len(findings) >= maxReviewFindings {
			break
		}
	}
	return findings
}

func looksLikeHTMLDocument(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype html")
}

func appendHTMLDocumentFindings(findings []string, path, content string) []string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
	default:
		return findings
	}
	if !looksLikeHTMLDocument(content) {
		return findings
	}
	if !htmlTitleRe.MatchString(content) {
		findings = append(findings, "HTML document is missing a <title>")
	}
	if !htmlViewportRe.MatchString(content) {
		findings = append(findings, "HTML document is missing a viewport meta tag")
	}
	return findings
}

func appendHTMLLinkFindings(findings []string, content string) []string {
	ids := map[string]struct{}{}
	for _, match := range htmlIDRe.FindAllStringSubmatch(content, -1) {
		id := strings.TrimSpace(match[1])
		if id != "" {
			ids[id] = struct{}{}
		}
	}
	counts := map[string]int{}
	seenMissing := map[string]struct{}{}
	for _, match := range htmlHrefHashRe.FindAllStringSubmatch(content, -1) {
		target := strings.TrimSpace(match[1])
		if target == "" {
			continue
		}
		counts[target]++
		if _, ok := ids[target]; ok {
			continue
		}
		if _, seen := seenMissing[target]; seen {
			continue
		}
		seenMissing[target] = struct{}{}
		findings = append(findings, fmt.Sprintf("in-page link #%s has no matching id", target))
	}
	targets := make([]string, 0, len(counts))
	for target, count := range counts {
		if count >= 3 {
			targets = append(targets, target)
		}
	}
	sort.Strings(targets)
	for _, target := range targets {
		findings = append(findings, fmt.Sprintf("%d in-page links share the same target #%s", counts[target], target))
	}
	return findings
}

func appendPatchReviews(base string, works []*patchFileWork) string {
	reviewed := 0
	var extra strings.Builder
	for _, work := range works {
		if work == nil || work.action == "delete" {
			continue
		}
		if reviewed >= maxReviewFiles {
			extra.WriteString("\n\n[review_file] remaining written files omitted")
			break
		}
		extra.WriteString("\n\n")
		extra.WriteString(formatReview(work.path, work.after))
		reviewed++
	}
	if extra.Len() == 0 {
		return base
	}
	return base + extra.String()
}
