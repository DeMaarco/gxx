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

const maxPatchResultBytes = 16 * 1024 * 1024

type applyPatchArgs struct {
	Patch string `json:"patch"`
}

func (r *Registry) applyPatchSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name: "apply_patch",
			Description: `Preferred way to update existing files. Apply an approved multi-file patch as one transaction. Format:
*** Begin Patch
*** Add File: path
+new line
*** Update File: path
@@
 context line
-old line
+new line
*** Delete File: path
*** End Patch
Prefix unchanged hunk lines with one space. Use "*** End of File" after the last content line only when the file has no final newline.`,
			ReadOnly: false,
			Parameters: objectSchema(map[string]any{
				"patch": map[string]any{
					"type":        "string",
					"description": "Complete Begin Patch/End Patch document containing one or more file operations.",
				},
			}, "patch"),
		},
		prepare: r.prepareApplyPatch,
	}
}

func (r *Registry) prepareApplyPatch(
	raw json.RawMessage,
) (approval.Action, toolRun, error) {
	var args applyPatchArgs
	if err := decodeArgs(raw, &args); err != nil {
		return approval.Action{}, nil, err
	}
	operations, err := parsePatch(args.Patch)
	if err != nil {
		return approval.Action{}, nil, err
	}

	changes := make([]workspace.FileChange, 0, len(operations))
	paths := make([]string, 0, len(operations))
	seen := make(map[string]struct{}, len(operations))
	var preview strings.Builder
	totalBytes := 0

	for _, operation := range operations {
		clean, err := r.workspace.Clean(operation.path)
		if err != nil {
			return approval.Action{}, nil, fmt.Errorf("%s: %w", operation.path, err)
		}
		if clean != operation.path || strings.TrimSpace(operation.path) != operation.path {
			return approval.Action{}, nil, fmt.Errorf(
				"patch path must already be normalized: %q",
				operation.path,
			)
		}
		if isSensitivePath(clean) {
			return approval.Action{}, nil, fmt.Errorf(
				"refusing to patch sensitive path: %s",
				clean,
			)
		}
		if _, exists := seen[clean]; exists {
			return approval.Action{}, nil, fmt.Errorf("duplicate patch path %q", clean)
		}
		seen[clean] = struct{}{}

		change := workspace.FileChange{Path: clean}
		var before, after []byte
		switch operation.kind {
		case patchAdd:
			if _, err := r.workspace.Lstat(clean); err == nil {
				return approval.Action{}, nil, fmt.Errorf("cannot add existing path %q", clean)
			} else if !errors.Is(err, os.ErrNotExist) {
				return approval.Action{}, nil, fmt.Errorf("inspect %s: %w", clean, err)
			}
			after = append([]byte(nil), operation.data...)
			change.Data = after

		case patchUpdate:
			before, err = r.workspace.ReadRegularFile(clean, maxEditableFile)
			if err != nil {
				return approval.Action{}, nil, err
			}
			after, err = applyPatchHunks(before, operation)
			if err != nil {
				return approval.Action{}, nil, err
			}
			if bytes.Equal(before, after) {
				return approval.Action{}, nil, fmt.Errorf("patch does not change %s", clean)
			}
			change.Data = after
			change.Expected = before
			change.ExpectedExists = true

		case patchDelete:
			before, err = r.workspace.ReadRegularFile(clean, maxEditableFile)
			if err != nil {
				return approval.Action{}, nil, err
			}
			change.Delete = true
			change.Expected = before
			change.ExpectedExists = true

		default:
			return approval.Action{}, nil, errors.New("unsupported patch operation")
		}

		totalBytes += len(before) + len(after)
		if totalBytes > maxPatchResultBytes {
			return approval.Action{}, nil, fmt.Errorf(
				"patch transaction exceeds %d bytes",
				maxPatchResultBytes,
			)
		}
		if preview.Len() > 0 {
			preview.WriteString("\n\n")
		}
		preview.WriteString(compactDiff(clean, string(before), string(after)))
		changes = append(changes, change)
		paths = append(paths, clean)
	}

	action := approval.Action{
		Title:   fmt.Sprintf("Apply patch to %d file(s)", len(changes)),
		Preview: approval.CapPreview(preview.String()),
		Kind:    approval.KindWrite,
	}
	run := func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := r.workspace.ApplyTransaction(changes); err != nil {
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
