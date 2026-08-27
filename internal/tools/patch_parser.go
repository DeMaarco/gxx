package tools

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

const (
	maxPatchBytes      = 512 * 1024
	maxPatchOperations = 50
	maxPatchHunks      = 200
)

type patchOperationKind string

const (
	patchAdd    patchOperationKind = "add"
	patchUpdate patchOperationKind = "update"
	patchDelete patchOperationKind = "delete"
)

type patchOperation struct {
	kind  patchOperationKind
	path  string
	data  []byte
	hunks []patchHunk
	noEOL bool
}

type patchHunk struct {
	header string
	lines  []patchLine
	noEOL  bool
}

type patchLine struct {
	kind byte
	text string
}

func parsePatch(value string) ([]patchOperation, error) {
	if len(value) > maxPatchBytes {
		return nil, fmt.Errorf("patch exceeds %d bytes", maxPatchBytes)
	}
	if strings.ContainsRune(value, 0) {
		return nil, errors.New("patch contains a NUL byte")
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(value, "\n")

	index := 0
	for index < len(lines) && lines[index] == "" {
		index++
	}
	if index >= len(lines) || lines[index] != "*** Begin Patch" {
		return nil, errors.New(`patch must start with "*** Begin Patch"`)
	}
	index++

	var operations []patchOperation
	totalHunks := 0
	for {
		for index < len(lines) && lines[index] == "" {
			index++
		}
		if index >= len(lines) {
			return nil, errors.New(`patch is missing "*** End Patch"`)
		}
		if lines[index] == "*** End Patch" {
			index++
			for index < len(lines) {
				if lines[index] != "" {
					return nil, errors.New("unexpected content after patch end")
				}
				index++
			}
			break
		}
		if len(operations) >= maxPatchOperations {
			return nil, fmt.Errorf("patch exceeds %d file operations", maxPatchOperations)
		}

		operation, next, hunks, err := parsePatchOperation(lines, index)
		if err != nil {
			return nil, err
		}
		totalHunks += hunks
		if totalHunks > maxPatchHunks {
			return nil, fmt.Errorf("patch exceeds %d hunks", maxPatchHunks)
		}
		operations = append(operations, operation)
		index = next
	}

	if len(operations) == 0 {
		return nil, errors.New("patch contains no file operations")
	}
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if _, exists := seen[operation.path]; exists {
			return nil, fmt.Errorf("patch contains duplicate path %q", operation.path)
		}
		seen[operation.path] = struct{}{}
	}
	return operations, nil
}

func parsePatchOperation(
	lines []string,
	index int,
) (patchOperation, int, int, error) {
	kind, path, ok := parsePatchHeader(lines[index])
	if !ok {
		return patchOperation{}, index, 0, fmt.Errorf(
			"line %d: expected an Add, Update, or Delete File header",
			index+1,
		)
	}
	if strings.TrimSpace(path) == "" {
		return patchOperation{}, index, 0, fmt.Errorf("line %d: file path cannot be empty", index+1)
	}
	operation := patchOperation{kind: kind, path: path}
	index++

	switch kind {
	case patchAdd:
		var content []string
		for index < len(lines) && !isPatchBoundary(lines[index]) {
			line := lines[index]
			if line == "*** End of File" {
				operation.noEOL = true
				index++
				if index < len(lines) && !isPatchBoundary(lines[index]) && lines[index] != "" {
					return patchOperation{}, index, 0, fmt.Errorf(
						"line %d: End of File must finish an operation",
						index+1,
					)
				}
				break
			}
			if line == "" || line[0] != '+' {
				return patchOperation{}, index, 0, fmt.Errorf(
					"line %d: added file lines must start with +",
					index+1,
				)
			}
			content = append(content, line[1:])
			index++
		}
		operation.data = []byte(strings.Join(content, "\n"))
		if len(content) > 0 && !operation.noEOL {
			operation.data = append(operation.data, '\n')
		}
		return operation, index, 0, nil

	case patchDelete:
		if index < len(lines) && !isPatchBoundary(lines[index]) && lines[index] != "" {
			return patchOperation{}, index, 0, fmt.Errorf(
				"line %d: Delete File does not accept content",
				index+1,
			)
		}
		return operation, index, 0, nil

	case patchUpdate:
		for index < len(lines) && !isPatchBoundary(lines[index]) {
			if lines[index] == "" {
				return patchOperation{}, index, 0, fmt.Errorf(
					"line %d: hunk lines require a prefix",
					index+1,
				)
			}
			if lines[index] == "*** End of File" {
				if len(operation.hunks) == 0 {
					return patchOperation{}, index, 0, fmt.Errorf(
						"line %d: End of File requires a hunk",
						index+1,
					)
				}
				operation.noEOL = true
				operation.hunks[len(operation.hunks)-1].noEOL = true
				index++
				if index < len(lines) && !isPatchBoundary(lines[index]) && lines[index] != "" {
					return patchOperation{}, index, 0, fmt.Errorf(
						"line %d: End of File must finish an operation",
						index+1,
					)
				}
				break
			}
			if !strings.HasPrefix(lines[index], "@@") {
				return patchOperation{}, index, 0, fmt.Errorf(
					"line %d: update content must start with a @@ hunk",
					index+1,
				)
			}
			hunk := patchHunk{header: lines[index]}
			index++
			hasChange := false
			for index < len(lines) &&
				!isPatchBoundary(lines[index]) &&
				!strings.HasPrefix(lines[index], "@@") &&
				lines[index] != "*** End of File" {
				line := lines[index]
				if line == "" || (line[0] != ' ' && line[0] != '+' && line[0] != '-') {
					return patchOperation{}, index, 0, fmt.Errorf(
						"line %d: hunk lines must start with space, +, or -",
						index+1,
					)
				}
				hunk.lines = append(hunk.lines, patchLine{kind: line[0], text: line[1:]})
				if line[0] == '+' || line[0] == '-' {
					hasChange = true
				}
				index++
			}
			if len(hunk.lines) == 0 || !hasChange {
				return patchOperation{}, index, 0, fmt.Errorf(
					"line %d: hunk must contain at least one change",
					index+1,
				)
			}
			operation.hunks = append(operation.hunks, hunk)
		}
		if len(operation.hunks) == 0 {
			return patchOperation{}, index, 0, fmt.Errorf(
				"update for %q contains no hunks",
				path,
			)
		}
		return operation, index, len(operation.hunks), nil
	}

	return patchOperation{}, index, 0, errors.New("unknown patch operation")
}

func parsePatchHeader(line string) (patchOperationKind, string, bool) {
	for _, candidate := range []struct {
		prefix string
		kind   patchOperationKind
	}{
		{prefix: "*** Add File: ", kind: patchAdd},
		{prefix: "*** Update File: ", kind: patchUpdate},
		{prefix: "*** Delete File: ", kind: patchDelete},
	} {
		if strings.HasPrefix(line, candidate.prefix) {
			return candidate.kind, strings.TrimPrefix(line, candidate.prefix), true
		}
	}
	return "", "", false
}

func isPatchBoundary(line string) bool {
	if line == "*** End Patch" {
		return true
	}
	_, _, ok := parsePatchHeader(line)
	return ok
}

func applyPatchHunks(current []byte, operation patchOperation) ([]byte, error) {
	if operation.kind != patchUpdate {
		return nil, errors.New("cannot apply hunks for a non-update operation")
	}
	if bytes.IndexByte(current, 0) >= 0 {
		return nil, fmt.Errorf("refusing to patch binary file %q", operation.path)
	}

	updated := string(current)
	for index, hunk := range operation.hunks {
		oldBlock, newBlock := hunkBlocks(hunk)
		if oldBlock == "" {
			return nil, fmt.Errorf("%s hunk %d has no context or removed text", operation.path, index+1)
		}
		next, matches := replaceUniqueLineBlock(updated, oldBlock, newBlock)
		if matches != 1 {
			return nil, fmt.Errorf(
				"%s hunk %d must match exactly once; found %d matches",
				operation.path,
				index+1,
				matches,
			)
		}
		updated = next
	}
	return []byte(updated), nil
}

// replaceUniqueLineBlock replaces oldBlock with newBlock only when oldBlock
// occurs once as a complete line sequence. A match must start at the beginning
// of the file or after a newline, and must end at EOF or on a newline so a
// hunk cannot rewrite a unique substring inside a longer line.
func replaceUniqueLineBlock(content, oldBlock, newBlock string) (string, int) {
	matches := 0
	index := -1
	searchFrom := 0
	for searchFrom <= len(content) {
		found := indexCompleteLineBlock(content, oldBlock, searchFrom)
		if found < 0 {
			break
		}
		matches++
		if matches == 1 {
			index = found
		}
		searchFrom = found + 1
	}
	if matches != 1 {
		return content, matches
	}
	return content[:index] + newBlock + content[index+len(oldBlock):], matches
}

func indexCompleteLineBlock(content, block string, start int) int {
	if block == "" || start > len(content) {
		return -1
	}
	for start <= len(content) {
		relative := strings.Index(content[start:], block)
		if relative < 0 {
			return -1
		}
		index := start + relative
		if isCompleteLineBlock(content, index, index+len(block)) {
			return index
		}
		start = index + 1
	}
	return -1
}

func isCompleteLineBlock(content string, start, end int) bool {
	if start < 0 || end < start || end > len(content) {
		return false
	}
	if start > 0 && content[start-1] != '\n' {
		return false
	}
	if end == len(content) {
		return true
	}
	return end > start && content[end-1] == '\n'
}

func hunkBlocks(hunk patchHunk) (string, string) {
	var oldBlock strings.Builder
	var newBlock strings.Builder
	for _, line := range hunk.lines {
		if line.kind == ' ' || line.kind == '-' {
			oldBlock.WriteString(line.text)
			oldBlock.WriteByte('\n')
		}
		if line.kind == ' ' || line.kind == '+' {
			newBlock.WriteString(line.text)
			newBlock.WriteByte('\n')
		}
	}
	oldValue := oldBlock.String()
	newValue := newBlock.String()
	if hunk.noEOL {
		oldValue = strings.TrimSuffix(oldValue, "\n")
		newValue = strings.TrimSuffix(newValue, "\n")
	}
	return oldValue, newValue
}
