package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"gxx/internal/agent"
	"gxx/internal/approval"
)

const (
	defaultListDepth = 4
	maxListDepth     = 12
	maxListEntries   = 1000
	defaultReadLines = 200
	maxReadLines     = 1000
	maxScannedFile   = 2 * 1024 * 1024
	maxEditableFile  = 4 * 1024 * 1024
	maxWriteBytes    = 1024 * 1024
)

var ignoredDirectories = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	".cache":       {},
	".idea":        {},
	".next":        {},
	".venv":        {},
	"__pycache__":  {},
	"bin":          {},
	"build":        {},
	"coverage":     {},
	"dist":         {},
	"node_modules": {},
	"target":       {},
	"vendor":       {},
	"venv":         {},
}

func (r *Registry) listFilesSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "list_files",
			Description: "List files and directories under a workspace-relative path. Common dependency and build directories are skipped.",
			ReadOnly:    true,
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        []string{"string", "null"},
					"description": "Relative directory to list, or null for the workspace root.",
				},
				"max_depth": map[string]any{
					"type":        []string{"integer", "null"},
					"description": "Maximum recursion depth from 1 to 12, or null for 4.",
				},
			}, "path", "max_depth"),
		},
		run: r.listFiles,
	}
}

func (r *Registry) searchFilesSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "search_files",
			Description: "Search regular text files for a case-insensitive literal string and return matching lines.",
			ReadOnly:    true,
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Non-empty literal text to find.",
				},
				"path": map[string]any{
					"type":        []string{"string", "null"},
					"description": "Relative file or directory to search, or null for the workspace root.",
				},
				"max_results": map[string]any{
					"type":        []string{"integer", "null"},
					"description": "Maximum matches to return, or null for the configured limit.",
				},
			}, "query", "path", "max_results"),
		},
		run: r.searchFiles,
	}
}

func (r *Registry) readFileSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "read_file",
			Description: "Read a range of lines from a workspace-relative text file. Lines are returned with line numbers.",
			ReadOnly:    true,
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative file path.",
				},
				"offset_line": map[string]any{
					"type":        []string{"integer", "null"},
					"description": "One-based first line, or null for line 1.",
				},
				"limit_lines": map[string]any{
					"type":        []string{"integer", "null"},
					"description": "Number of lines from 1 to 1000, or null for 200.",
				},
			}, "path", "offset_line", "limit_lines"),
		},
		run: r.readFile,
	}
}

func (r *Registry) editFileSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "edit_file",
			Description: "Replace exactly one occurrence of old_text in an existing workspace file. Requires user approval.",
			ReadOnly:    false,
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative file path.",
				},
				"old_text": map[string]any{
					"type":        "string",
					"description": "Exact non-empty text that must occur once.",
				},
				"new_text": map[string]any{
					"type":        "string",
					"description": "Replacement text.",
				},
			}, "path", "old_text", "new_text"),
		},
		prepare: r.prepareEditFile,
	}
}

func (r *Registry) writeFileSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "write_file",
			Description: "Create or completely replace a workspace file, creating parent directories as needed. Requires user approval.",
			ReadOnly:    false,
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative file path.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Complete new file contents.",
				},
			}, "path", "content"),
		},
		prepare: r.prepareWriteFile,
	}
}

type listFilesArgs struct {
	Path     *string `json:"path"`
	MaxDepth *int    `json:"max_depth"`
}

func (r *Registry) listFiles(ctx context.Context, raw json.RawMessage) (string, error) {
	var args listFilesArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	path := optionalString(args.Path, ".")
	depth := optionalInt(args.MaxDepth, defaultListDepth)
	if depth < 1 || depth > maxListDepth {
		return "", fmt.Errorf("max_depth must be between 1 and %d", maxListDepth)
	}

	root, err := r.workspace.Clean(path)
	if err != nil {
		return "", err
	}
	info, err := r.workspace.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", path)
	}

	var entries []string
	err = iofs.WalkDir(r.workspace.FS(), root, func(current string, entry iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == root {
			return nil
		}

		relativeToRoot, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		currentDepth := strings.Count(relativeToRoot, "/") + 1
		if entry.IsDir() && isIgnoredDirectory(entry.Name()) {
			return iofs.SkipDir
		}
		if currentDepth > depth {
			if entry.IsDir() {
				return iofs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if _, err := r.workspace.Stat(current); err != nil {
				return nil
			}
		}

		display := current
		if entry.IsDir() {
			display += "/"
		}
		entries = append(entries, display)
		if len(entries) >= maxListEntries {
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return "", err
	}
	if len(entries) == 0 {
		return "No files found.", nil
	}
	if len(entries) == maxListEntries {
		entries = append(entries, "… file list limited by gxx")
	}
	return strings.Join(entries, "\n"), nil
}

type searchFilesArgs struct {
	Query      string  `json:"query"`
	Path       *string `json:"path"`
	MaxResults *int    `json:"max_results"`
}

func (r *Registry) searchFiles(ctx context.Context, raw json.RawMessage) (string, error) {
	var args searchFilesArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", errors.New("query cannot be empty")
	}
	path := optionalString(args.Path, ".")
	limit := optionalInt(args.MaxResults, r.maxSearchResults)
	if limit < 1 {
		return "", errors.New("max_results must be positive")
	}
	limit = min(limit, r.maxSearchResults)

	target, err := r.workspace.Clean(path)
	if err != nil {
		return "", err
	}

	var matches []string
	searchOne := func(file string) error {
		if isSensitivePath(file) {
			return nil
		}
		found, err := r.searchTextFile(ctx, file, query, limit-len(matches))
		if err != nil {
			return err
		}
		matches = append(matches, found...)
		return nil
	}

	info, err := r.workspace.Stat(target)
	if err != nil {
		return "", err
	}
	if info.Mode().IsRegular() {
		if isSensitivePath(target) {
			return "", fmt.Errorf("refusing to search sensitive path: %s", path)
		}
		if err := searchOne(target); err != nil {
			return "", err
		}
	} else if info.IsDir() {
		err = iofs.WalkDir(r.workspace.FS(), target, func(current string, entry iofs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				if current != target && isIgnoredDirectory(entry.Name()) {
					return iofs.SkipDir
				}
				return nil
			}
			if len(matches) >= limit {
				return errStopWalk
			}
			if isSensitivePath(current) {
				return nil
			}
			fileInfo, err := r.workspace.Stat(current)
			if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() > maxScannedFile {
				return nil
			}
			return searchOne(current)
		})
		if err != nil && !errors.Is(err, errStopWalk) {
			return "", err
		}
	} else {
		return "", fmt.Errorf("not a regular file or directory: %s", path)
	}

	if len(matches) == 0 {
		return "No matches found.", nil
	}
	sort.Strings(matches)
	if len(matches) >= limit {
		matches = append(matches, "… search result limit reached")
	}
	return strings.Join(matches, "\n"), nil
}

type readFileArgs struct {
	Path       string `json:"path"`
	OffsetLine *int   `json:"offset_line"`
	LimitLines *int   `json:"limit_lines"`
}

func (r *Registry) readFile(ctx context.Context, raw json.RawMessage) (string, error) {
	var args readFileArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	offset := optionalInt(args.OffsetLine, 1)
	limit := optionalInt(args.LimitLines, defaultReadLines)
	if offset < 1 {
		return "", errors.New("offset_line must be at least 1")
	}
	if limit < 1 || limit > maxReadLines {
		return "", fmt.Errorf("limit_lines must be between 1 and %d", maxReadLines)
	}

	if isSensitivePath(args.Path) {
		return "", fmt.Errorf("refusing to read sensitive path: %s", args.Path)
	}
	file, err := r.workspace.OpenRegular(args.Path, maxEditableFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	binary, err := isBinary(file)
	if err != nil {
		return "", err
	}
	if binary {
		return "", fmt.Errorf("refusing to read binary file: %s", args.Path)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var output strings.Builder
	lineNumber := 0
	written := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		lineNumber++
		if lineNumber < offset {
			continue
		}
		if written >= limit {
			break
		}
		fmt.Fprintf(&output, "%6d|%s\n", lineNumber, scanner.Text())
		written++
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if written == 0 {
		return fmt.Sprintf("No lines at or after line %d.", offset), nil
	}
	return strings.TrimSuffix(output.String(), "\n"), nil
}

type editFileArgs struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

func (r *Registry) prepareEditFile(raw json.RawMessage) (approval.Action, toolRun, error) {
	args, current, proposed, err := r.proposedEdit(raw)
	if err != nil {
		return approval.Action{}, nil, err
	}
	action := approval.Action{
		Title:   "Edit " + args.Path,
		Preview: compactDiff(args.Path, string(current), string(proposed)),
		Kind:    approval.KindWrite,
	}
	run := func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		latest, err := r.workspace.ReadRegularFile(args.Path, maxEditableFile)
		if err != nil {
			return "", err
		}
		if !bytes.Equal(latest, current) {
			return "", errors.New("file changed after approval; edit was not applied")
		}
		if err := r.workspace.AtomicWrite(args.Path, proposed); err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated %s", args.Path), nil
	}
	return action, run, nil
}

func (r *Registry) proposedEdit(raw json.RawMessage) (editFileArgs, []byte, []byte, error) {
	var args editFileArgs
	if err := decodeArgs(raw, &args); err != nil {
		return args, nil, nil, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return args, nil, nil, errors.New("path cannot be empty")
	}
	if isSensitivePath(args.Path) {
		return args, nil, nil, fmt.Errorf("refusing to edit sensitive path: %s", args.Path)
	}
	if args.OldText == "" {
		return args, nil, nil, errors.New("old_text cannot be empty")
	}
	if len(args.OldText)+len(args.NewText) > maxEditableFile {
		return args, nil, nil, errors.New("edit payload is too large")
	}
	current, err := r.workspace.ReadRegularFile(args.Path, maxEditableFile)
	if err != nil {
		return args, nil, nil, err
	}
	count := bytes.Count(current, []byte(args.OldText))
	if count != 1 {
		return args, nil, nil, fmt.Errorf("old_text must occur exactly once; found %d occurrences", count)
	}
	proposed := bytes.Replace(current, []byte(args.OldText), []byte(args.NewText), 1)
	return args, current, proposed, nil
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (r *Registry) prepareWriteFile(raw json.RawMessage) (approval.Action, toolRun, error) {
	var args writeFileArgs
	if err := decodeArgs(raw, &args); err != nil {
		return approval.Action{}, nil, err
	}
	if err := validateWriteArgs(args); err != nil {
		return approval.Action{}, nil, err
	}
	if _, err := r.workspace.Clean(args.Path); err != nil {
		return approval.Action{}, nil, err
	}
	current, existed, err := r.fileSnapshot(args.Path)
	if err != nil {
		return approval.Action{}, nil, err
	}
	proposed := []byte(args.Content)
	action := approval.Action{
		Title:   "Write " + args.Path,
		Preview: compactDiff(args.Path, string(current), args.Content),
		Kind:    approval.KindWrite,
	}
	run := func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		latest, stillExists, err := r.fileSnapshot(args.Path)
		if err != nil {
			return "", err
		}
		if stillExists != existed || !bytes.Equal(latest, current) {
			return "", errors.New("file changed after approval; write was not applied")
		}
		if err := r.workspace.AtomicWrite(args.Path, proposed); err != nil {
			return "", err
		}
		return fmt.Sprintf("Wrote %s", args.Path), nil
	}
	return action, run, nil
}

func validateWriteArgs(args writeFileArgs) error {
	if strings.TrimSpace(args.Path) == "" {
		return errors.New("path cannot be empty")
	}
	if isSensitivePath(args.Path) {
		return fmt.Errorf("refusing to write sensitive path: %s", args.Path)
	}
	if len(args.Content) > maxWriteBytes {
		return fmt.Errorf("content exceeds %d bytes", maxWriteBytes)
	}
	return nil
}

func (r *Registry) fileSnapshot(path string) ([]byte, bool, error) {
	current, err := r.workspace.ReadRegularFile(path, maxEditableFile)
	if err == nil {
		return current, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

func compactDiff(path, oldValue, newValue string) string {
	oldLines := strings.Split(oldValue, "\n")
	newLines := strings.Split(newValue, "\n")
	if oldValue == "" {
		oldLines = nil
	}
	if newValue == "" {
		newLines = nil
	}

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix &&
		suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	var output strings.Builder
	fmt.Fprintf(&output, "--- %s\n+++ %s\n", path, path)
	contextStart := max(prefix-2, 0)
	for _, line := range oldLines[contextStart:prefix] {
		fmt.Fprintf(&output, " %s\n", line)
	}
	for _, line := range oldLines[prefix : len(oldLines)-suffix] {
		fmt.Fprintf(&output, "-%s\n", line)
	}
	for _, line := range newLines[prefix : len(newLines)-suffix] {
		fmt.Fprintf(&output, "+%s\n", line)
	}
	for _, line := range oldLines[len(oldLines)-suffix : min(len(oldLines)-suffix+2, len(oldLines))] {
		fmt.Fprintf(&output, " %s\n", line)
	}
	return strings.TrimSuffix(output.String(), "\n")
}

func (r *Registry) searchTextFile(
	ctx context.Context,
	path string,
	query string,
	limit int,
) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	file, err := r.workspace.OpenRegular(path, maxScannedFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	binary, err := isBinary(file)
	if err != nil || binary {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	needle := strings.ToLower(query)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var matches []string
	line := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line++
		text := scanner.Text()
		if strings.Contains(strings.ToLower(text), needle) {
			text = truncateLine(text, 300)
			matches = append(matches, fmt.Sprintf("%s:%d:%s", path, line, text))
			if len(matches) >= limit {
				break
			}
		}
	}
	return matches, scanner.Err()
}

func isBinary(file *os.File) (bool, error) {
	buffer := make([]byte, 8192)
	read, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false, err
	}
	return bytes.IndexByte(buffer[:read], 0) >= 0, nil
}

func truncateLine(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func isIgnoredDirectory(name string) bool {
	_, ignored := ignoredDirectories[name]
	return ignored
}

func isSensitivePath(value string) bool {
	clean := strings.ToLower(pathpkg.Clean(strings.ReplaceAll(value, "\\", "/")))
	base := pathpkg.Base(clean)
	if clean == ".git" || strings.HasPrefix(clean, ".git/") || strings.Contains(clean, "/.git/") {
		return true
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	switch base {
	case ".netrc", ".npmrc", ".pypirc", "credentials", "credentials.json",
		"id_dsa", "id_ecdsa", "id_ed25519", "id_rsa":
		return true
	}
	for _, suffix := range []string{".jks", ".key", ".p12", ".pem", ".pfx"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func decodeArgs(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		return errors.New("tool arguments are empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid tool arguments: trailing JSON")
	}
	return nil
}

func optionalString(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return *value
}

func optionalInt(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

var errStopWalk = errors.New("stop walking")
