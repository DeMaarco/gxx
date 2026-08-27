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
	"os"
	"strings"

	"gxx/internal/agent"
	"gxx/internal/approval"
	"gxx/internal/workspace"
)

const (
	maxPatchResultBytes = 16 * 1024 * 1024
	maxPatchChanges     = 50
)

type applyPatchArgs struct {
	Changes []patchChange `json:"changes"`
}

type patchChange struct {
	Path    string  `json:"path"`
	Action  string  `json:"action"`
	Content *string `json:"content"`
	OldText *string `json:"old_text"`
	NewText *string `json:"new_text"`
}

type patchFileWork struct {
	path   string
	action string
	before []byte
	after  []byte
}

func (r *Registry) applyPatchSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name: "apply_patch",
			Description: `Create, update, or delete workspace files in one approved transaction.
Pass changes as an array of objects:
- action add: create a new file; content is the full file; fails if the path exists.
- action update: replace old_text with new_text; old_text must occur exactly once. Multiple updates to the same path apply in order.
- action delete: remove an existing file.
Do not mix add, update, and delete on the same path. Prefer one apply_patch call for related files.`,
			ReadOnly: false,
			Parameters: objectSchema(map[string]any{
				"changes": map[string]any{
					"type": "array",
					"items": objectSchema(map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Workspace-relative file path.",
						},
						"action": map[string]any{
							"type":        "string",
							"description": "add, update, or delete.",
						},
						"content": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Full file contents for add, or null otherwise.",
						},
						"old_text": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Exact text to replace for update, or null otherwise. Must occur once.",
						},
						"new_text": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Replacement text for update, or null otherwise.",
						},
					}, "path", "action", "content", "old_text", "new_text"),
					"description": "File operations to apply together.",
				},
			}, "changes"),
		},
		prepare: r.prepareApplyPatch,
	}
}

func (r *Registry) prepareApplyPatch(
	raw json.RawMessage,
) (approval.Action, toolRun, error) {
	if err := rejectLegacyPatchDocument(raw); err != nil {
		return approval.Action{}, nil, err
	}
	var args applyPatchArgs
	if err := decodeArgs(raw, &args); err != nil {
		return approval.Action{}, nil, err
	}
	if len(args.Changes) == 0 {
		return approval.Action{}, nil, errors.New("changes cannot be empty")
	}
	if len(args.Changes) > maxPatchChanges {
		return approval.Action{}, nil, fmt.Errorf("changes exceeds %d operations", maxPatchChanges)
	}

	works := make([]*patchFileWork, 0, len(args.Changes))
	byPath := make(map[string]*patchFileWork, len(args.Changes))
	var totalBytes int
	for _, change := range args.Changes {
		work, err := r.applyPatchChange(byPath, &works, change)
		if err != nil {
			return approval.Action{}, nil, err
		}
		totalBytes += len(work.before) + len(work.after)
		if totalBytes > maxPatchResultBytes {
			return approval.Action{}, nil, fmt.Errorf(
				"patch transaction exceeds %d bytes",
				maxPatchResultBytes,
			)
		}
	}

	fileChanges := make([]workspace.FileChange, 0, len(works))
	paths := make([]string, 0, len(works))
	var preview strings.Builder
	for _, work := range works {
		if work.action == "update" && bytes.Equal(work.before, work.after) {
			return approval.Action{}, nil, fmt.Errorf("patch does not change %s", work.path)
		}
		change := workspace.FileChange{Path: work.path}
		switch work.action {
		case "add":
			change.Data = work.after
		case "update":
			change.Data = work.after
			change.Expected = work.before
			change.ExpectedExists = true
		case "delete":
			change.Delete = true
			change.Expected = work.before
			change.ExpectedExists = true
		}
		if preview.Len() > 0 {
			preview.WriteString("\n\n")
		}
		preview.WriteString(compactDiff(work.path, string(work.before), string(work.after)))
		fileChanges = append(fileChanges, change)
		paths = append(paths, work.path)
	}

	action := approval.Action{
		Title:   fmt.Sprintf("Apply patch to %d file(s)", len(fileChanges)),
		Preview: approval.CapPreview(preview.String()),
		Kind:    approval.KindWrite,
	}
	run := func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for _, path := range paths {
			reportProgress(ctx, path)
		}
		if err := r.workspace.ApplyTransaction(fileChanges); err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"Applied patch to %d file(s): %s",
			len(paths),
			strings.Join(paths, ", "),
		), nil
	}
	return action, run, nil
}

func rejectLegacyPatchDocument(raw json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	if _, ok := obj["patch"]; ok {
		return errors.New("apply_patch no longer accepts a patch document; pass changes as [{path, action, ...}]")
	}
	return nil
}

func (r *Registry) applyPatchChange(
	byPath map[string]*patchFileWork,
	works *[]*patchFileWork,
	change patchChange,
) (*patchFileWork, error) {
	action := strings.TrimSpace(change.Action)
	if action != "add" && action != "update" && action != "delete" {
		return nil, fmt.Errorf("unsupported action %q", change.Action)
	}
	if strings.TrimSpace(change.Path) == "" {
		return nil, errors.New("path cannot be empty")
	}
	clean, err := r.workspace.Clean(change.Path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", change.Path, err)
	}
	if clean != change.Path || strings.TrimSpace(change.Path) != change.Path {
		return nil, fmt.Errorf("patch path must already be normalized: %q", change.Path)
	}
	if isSensitivePath(clean) {
		return nil, fmt.Errorf("refusing to patch sensitive path: %s", clean)
	}

	work, seen := byPath[clean]
	if seen && work.action != action {
		return nil, fmt.Errorf("cannot mix %s and %s on %s", work.action, action, clean)
	}

	switch action {
	case "add":
		if seen {
			return nil, fmt.Errorf("duplicate add for %s", clean)
		}
		if change.Content == nil {
			return nil, fmt.Errorf("add requires content for %s", clean)
		}
		if len(*change.Content) > maxWriteBytes {
			return nil, fmt.Errorf("content exceeds %d bytes", maxWriteBytes)
		}
		if _, err := r.workspace.Lstat(clean); err == nil {
			return nil, fmt.Errorf("cannot add existing path %q", clean)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect %s: %w", clean, err)
		}
		work = &patchFileWork{path: clean, action: "add", after: []byte(*change.Content)}
		byPath[clean] = work
		*works = append(*works, work)

	case "update":
		oldText := ""
		if change.OldText != nil {
			oldText = *change.OldText
		}
		newText := ""
		if change.NewText != nil {
			newText = *change.NewText
		}
		if oldText == "" {
			return nil, fmt.Errorf("update requires old_text for %s", clean)
		}
		if len(oldText)+len(newText) > maxEditableFile {
			return nil, errors.New("edit payload is too large")
		}
		if !seen {
			before, err := r.workspace.ReadRegularFile(clean, maxEditableFile)
			if err != nil {
				return nil, err
			}
			work = &patchFileWork{path: clean, action: "update", before: before, after: append([]byte(nil), before...)}
			byPath[clean] = work
			*works = append(*works, work)
		}
		count := bytes.Count(work.after, []byte(oldText))
		if count != 1 {
			return nil, fmt.Errorf("old_text must occur exactly once in %s; found %d occurrences", clean, count)
		}
		work.after = bytes.Replace(work.after, []byte(oldText), []byte(newText), 1)

	case "delete":
		if seen {
			return nil, fmt.Errorf("duplicate delete for %s", clean)
		}
		before, err := r.workspace.ReadRegularFile(clean, maxEditableFile)
		if err != nil {
			return nil, err
		}
		work = &patchFileWork{path: clean, action: "delete", before: before}
		byPath[clean] = work
		*works = append(*works, work)
	}
	return work, nil
}
