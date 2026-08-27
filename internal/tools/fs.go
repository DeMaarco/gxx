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
	"regexp"
	"sort"
	"strings"

	"gxx/internal/agent"
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

func (r *Registry) listFilesSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "list_files",
			Description: "List files and directories under a workspace-relative path. Default dependency directories, .gitignore, and .gxxignore patterns are skipped.",
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
			Description: "Search regular text files and return matching lines as path:line:text. query is a RE2 regular expression; if it does not compile, it is searched as a literal string. Matching is case-insensitive unless case_sensitive is true. Optional glob limits files (gitignore style, for example *.go or **/*_test.go). Default dependency directories, .gitignore, and .gxxignore patterns are skipped.",
			ReadOnly:    true,
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Non-empty RE2 regular expression, or literal text if the pattern does not compile.",
				},
				"path": map[string]any{
					"type":        []string{"string", "null"},
					"description": "Relative file or directory to search, or null for the workspace root.",
				},
				"glob": map[string]any{
					"type":        []string{"string", "null"},
					"description": "Optional gitignore-style file filter, or null to search all text files.",
				},
				"max_results": map[string]any{
					"type":        []string{"integer", "null"},
					"description": "Maximum matches to return, or null for the configured limit.",
				},
				"case_sensitive": map[string]any{
					"type":        []string{"boolean", "null"},
					"description": "If true, match case-sensitively. Null or false is case-insensitive.",
				},
			}, "query", "path", "glob", "max_results", "case_sensitive"),
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

	matcher := r.ignoreForWalk(root)
	reportProgressNow(ctx, root)
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
		r.loadNestedIgnore(matcher, current, entry, root)
		ignored := matcher.ignores(current, entry.IsDir())
		if ignored && entry.IsDir() && !matcher.hasNegation {
			return iofs.SkipDir
		}
		if currentDepth > depth {
			if entry.IsDir() {
				return iofs.SkipDir
			}
			return nil
		}
		if ignored {
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
		reportProgress(ctx, display)
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
	Query         string  `json:"query"`
	Path          *string `json:"path"`
	Glob          *string `json:"glob"`
	MaxResults    *int    `json:"max_results"`
	CaseSensitive *bool   `json:"case_sensitive"`
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
	glob, err := compilePathGlob(optionalString(args.Glob, ""))
	if err != nil {
		return "", fmt.Errorf("invalid glob: %w", err)
	}
	matchLine := compileSearchMatcher(query, optionalBool(args.CaseSensitive, false))

	target, err := r.workspace.Clean(path)
	if err != nil {
		return "", err
	}

	matcher := r.ignoreForWalk(target)
	reportProgressNow(ctx, target)
	var matches []string
	searchOne := func(file string) error {
		if isSensitivePath(file) || matcher.ignores(file, false) || !matchPathGlob(glob, file) {
			return nil
		}
		reportProgress(ctx, file)
		found, err := r.searchTextFile(ctx, file, matchLine, limit-len(matches))
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
		if matcher.ignores(target, false) {
			return "No matches found.", nil
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
				r.loadNestedIgnore(matcher, current, entry, target)
				if current != target && matcher.ignores(current, true) && !matcher.hasNegation {
					return iofs.SkipDir
				}
				return nil
			}
			if len(matches) >= limit {
				return errStopWalk
			}
			if isSensitivePath(current) || matcher.ignores(current, false) || !matchPathGlob(glob, current) {
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

func compileSearchMatcher(query string, caseSensitive bool) func(string) bool {
	pattern := query
	if !caseSensitive {
		pattern = "(?i)" + query
	}
	if re, err := regexp.Compile(pattern); err == nil {
		return re.MatchString
	}
	if caseSensitive {
		return func(line string) bool {
			return strings.Contains(line, query)
		}
	}
	needle := strings.ToLower(query)
	return func(line string) bool {
		return strings.Contains(strings.ToLower(line), needle)
	}
}

func (r *Registry) searchTextFile(
	ctx context.Context,
	path string,
	match func(string) bool,
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
		if match(text) {
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

func optionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

var errStopWalk = errors.New("stop walking")
