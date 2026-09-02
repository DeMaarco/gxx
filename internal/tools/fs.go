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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gxx/internal/agent"
	"gxx/internal/workspace"
)

const (
	defaultListDepth    = 4
	maxListDepth        = 12
	maxListEntries      = 1000
	overviewListDepth   = 3
	overviewMaxEntries  = 40
	defaultReadLines    = 200
	maxReadLines        = 1000
	maxReadBytes        = 12 * 1024
	denseLineBytes      = 400
	maxSearchMatchBytes = 4 * 1024
	overviewSizeBytes   = 8 * 1024
	maxScannedFile      = 2 * 1024 * 1024
	maxEditableFile     = 4 * 1024 * 1024
	maxWriteBytes       = 1024 * 1024
)

func (r *Registry) listFilesSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name: "list_files",
			Description: "List files and directories under a workspace-relative path. Default dependency directories, .gitignore, .gxxignore patterns, and sensitive paths are skipped. " +
				"For package or folder inventories prefer path set and max_depth=1. Do not deep-list a large tree when names alone answer the question.",
			ReadOnly: true,
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        []string{"string", "null"},
					"description": "Relative directory to list, or null for the workspace root.",
				},
				"max_depth": map[string]any{
					"type":        []string{"integer", "null"},
					"description": "Maximum recursion depth from 1 to 12, or null for 4. Use 1 for top-level names only.",
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
			Description: "Search regular text files and return matching lines as path:line:text. query is a RE2 regular expression; if it does not compile, it is searched as a literal string. A bare identifier is matched as a whole word (CamelCase prefixes also match longer symbols, so ContextSizes matches ContextSizesForModel). ALL-CAPS tokens such as TODO or FIXME are case-sensitive unless case_sensitive is false. Matching is otherwise case-insensitive unless case_sensitive is true. Leave max_results null for the configured default; small caps hide useful hits. Optional glob limits files (gitignore style, for example *.go or **/*_test.go). Default dependency directories, .gitignore, and .gxxignore patterns are skipped.",
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
			Description: "Read a range of lines from a workspace-relative text file. Lines are returned with line numbers. Default reads stop around 12KB. Minified or densely packed files are sampled once; use search_files for a selector instead of paging with offset_line. Prefer search_files for a selector in a large CSS, JSON, or lockfile.",
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
	if isSensitivePath(root) {
		return "", refuseSensitive("list", path)
	}
	info, err := r.workspace.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", path)
	}

	matcher := r.ignoreForWalk(root)
	if matcher.ignores(root, true) {
		return fmt.Sprintf("%s is ignored by default ignore, .gitignore, or .gxxignore.", path), nil
	}

	entries, omitted, err := r.walkListedEntries(ctx, root, depth, maxListEntries, true, false)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		if omitted > 0 {
			return omittedSensitiveNotice(omitted), nil
		}
		return "No files found.", nil
	}
	if len(entries) == maxListEntries {
		entries = append(entries, "… file list limited by gxx")
	}
	if omitted > 0 {
		entries = append(entries, omittedSensitiveNotice(omitted))
	}
	return strings.Join(entries, "\n"), nil
}

// WorkspaceOverview is a cheap depth-3 listing for the start of each user turn.
func (r *Registry) WorkspaceOverview(ctx context.Context) string {
	if r == nil || r.workspace == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var builder strings.Builder
	builder.WriteString("[workspace]")
	if r.workspace.HasGit() {
		builder.WriteString("\ngit: yes")
	} else {
		builder.WriteString("\ngit: no")
	}
	root, err := r.workspace.Clean(".")
	if err != nil {
		return builder.String()
	}
	info, err := r.workspace.Stat(root)
	if err != nil || !info.IsDir() {
		return builder.String()
	}
	entries, _, err := r.walkListedEntries(ctx, root, overviewListDepth, overviewMaxEntries, false, true)
	if err != nil {
		return builder.String()
	}
	truncated := len(entries) >= overviewMaxEntries
	if len(entries) == 0 {
		builder.WriteString("\nfiles: 0")
	} else if truncated {
		fmt.Fprintf(&builder, "\nfiles: %d+ (depth %d)", len(entries), overviewListDepth)
	} else {
		fmt.Fprintf(&builder, "\nfiles: %d (depth %d)", len(entries), overviewListDepth)
	}
	for _, entry := range entries {
		builder.WriteByte('\n')
		builder.WriteString(entry)
	}
	if truncated {
		builder.WriteString("\n… truncated")
	}
	if ignored := r.topLevelIgnored(); len(ignored) > 0 {
		builder.WriteString("\nignored: ")
		builder.WriteString(strings.Join(ignored, ", "))
	}
	return builder.String()
}

func (r *Registry) topLevelIgnored() []string {
	if r == nil || r.workspace == nil {
		return nil
	}
	dirents, err := iofs.ReadDir(r.workspace.FS(), ".")
	if err != nil {
		return nil
	}
	matcher := r.ignoreForWalk(".")
	var names []string
	for _, entry := range dirents {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		if !matcher.ignores(name, entry.IsDir()) {
			continue
		}
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
		if len(names) >= 8 {
			break
		}
	}
	sort.Strings(names)
	return names
}

func (r *Registry) walkListedEntries(
	ctx context.Context,
	root string,
	depth, maxEntries int,
	progress, skipGenerated bool,
) ([]string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxEntries < 1 {
		maxEntries = 1
	}
	matcher := r.ignoreForWalk(root)
	if progress {
		reportProgressNow(ctx, root)
	}
	var entries []string
	omitted := 0
	err := iofs.WalkDir(r.workspace.FS(), root, func(current string, entry iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return workspace.Describe(current, walkErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == root {
			return nil
		}

		currentDepth, err := walkDepth(root, current)
		if err != nil {
			return err
		}
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
		if isSensitivePath(current) {
			omitted++
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
		if skipGenerated && r.skipGeneratedOverview(current, entry.IsDir()) {
			return nil
		}
		if skipGenerated && !entry.IsDir() {
			display = r.overviewDisplay(current)
		}
		if progress {
			reportProgress(ctx, display)
		}
		entries = append(entries, display)
		if len(entries) >= maxEntries {
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return nil, omitted, err
	}
	return entries, omitted, nil
}

// walkDepth is 1 for a direct child of root. Paths are slash-normalized so
// max_depth works on Windows, where filepath.Rel returns backslashes.
func walkDepth(root, current string) (int, error) {
	relative, err := filepath.Rel(filepath.FromSlash(root), filepath.FromSlash(current))
	if err != nil {
		return 0, err
	}
	relative = filepath.ToSlash(relative)
	if relative == "" || relative == "." {
		return 0, nil
	}
	return strings.Count(relative, "/") + 1, nil
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
	matchLine := compileSearchMatcher(query, args.CaseSensitive)

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
			return "", refuseSensitive("search", path)
		}
		if matcher.ignores(target, false) {
			return "No matches found.", nil
		}
		if err := searchOne(target); err != nil {
			return "", err
		}
	} else if info.IsDir() {
		if matcher.ignores(target, true) {
			return fmt.Sprintf("%s is ignored by default ignore, .gitignore, or .gxxignore.", path), nil
		}
		err = iofs.WalkDir(r.workspace.FS(), target, func(current string, entry iofs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return workspace.Describe(current, walkErr)
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
		return "", refuseSensitive("read", args.Path)
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
	dense, size := fileLooksDense(file)
	if dense && offset > 1 {
		kb := (size + 1023) / 1024
		if kb < 1 {
			kb = 1
		}
		return fmt.Sprintf("%s is dense (%dKB). Do not page it; this file was already sampled.", args.Path, kb), nil
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	budget := maxReadBytes
	var output strings.Builder
	lineNumber := 0
	written := 0
	stoppedEarly := false
	stoppedBytes := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		lineNumber++
		if lineNumber < offset {
			continue
		}
		if written >= limit {
			stoppedEarly = true
			break
		}
		line := scanner.Text()
		if output.Len() > 0 && output.Len()+len(line)+8 > budget {
			stoppedEarly = true
			stoppedBytes = true
			break
		}
		if len(line) > budget && written == 0 {
			line = clipToBytes(line, budget)
			stoppedBytes = true
			stoppedEarly = true
		}
		fmt.Fprintf(&output, "%6d|%s\n", lineNumber, line)
		written++
		if stoppedBytes {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if written == 0 {
		return fmt.Sprintf("No lines at or after line %d.", offset), nil
	}
	body := strings.TrimSuffix(output.String(), "\n")
	if stoppedEarly {
		nextOffset := lineNumber
		if stoppedBytes {
			if dense {
				return body + denseFileSuffix(scanner, lineNumber, size), nil
			}
			return fmt.Sprintf(
				"%s\n… truncated at %dKB; prefer search_files for more of this file; next offset_line=%d",
				body,
				maxReadBytes/1024,
				nextOffset,
			), nil
		}
		return fmt.Sprintf("%s\n… more lines follow; next offset_line=%d", body, nextOffset), nil
	}
	return fmt.Sprintf("%s\n(end of file, %d lines)", body, lineNumber), nil
}

func denseFileSuffix(scanner *bufio.Scanner, startLine int, size int64) string {
	kb := (size + 1023) / 1024
	if kb < 1 {
		kb = 1
	}
	var extra strings.Builder
	fmt.Fprintf(&extra, "\n… dense file (%dKB). Later markers:", kb)
	n := 0
	lineNumber := startLine
	for scanner.Scan() {
		lineNumber++
		text := scanner.Text()
		idx := strings.Index(text, "/*")
		if idx < 0 {
			continue
		}
		marker := strings.TrimSpace(text[idx:])
		if end := strings.Index(marker, "*/"); end >= 0 {
			marker = marker[:end+2]
		} else {
			marker = truncateLine(marker, 80)
		}
		fmt.Fprintf(&extra, "\n%d %s", lineNumber, marker)
		n++
		if n >= 20 {
			break
		}
	}
	if n == 0 {
		return fmt.Sprintf("\n… dense file (%dKB); this sample is enough to summarize", kb)
	}
	return extra.String()
}

func clipToBytes(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	keep := limit
	for keep > 0 && !utf8.RuneStart(value[keep]) {
		keep--
	}
	return value[:keep]
}

func (r *Registry) skipGeneratedOverview(path string, isDir bool) bool {
	if isDir || r == nil || r.workspace == nil {
		return false
	}
	if strings.HasSuffix(path, ".d.ts") {
		return true
	}
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	switch name {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb",
		"composer.lock", "Cargo.lock", "poetry.lock", "Gemfile.lock":
		return true
	}
	if !strings.HasSuffix(path, ".js") {
		return false
	}
	base := strings.TrimSuffix(path, ".js")
	if _, err := r.workspace.Stat(base + ".ts"); err == nil {
		return true
	}
	if _, err := r.workspace.Stat(base + ".tsx"); err == nil {
		return true
	}
	return false
}

func (r *Registry) overviewDisplay(path string) string {
	if r == nil || r.workspace == nil {
		return path
	}
	info, err := r.workspace.Stat(path)
	if err != nil || info.IsDir() || info.Size() < overviewSizeBytes {
		return path
	}
	return fmt.Sprintf("%s (%dKB)", path, info.Size()/1024)
}

func fileLooksDense(file *os.File) (bool, int64) {
	if file == nil {
		return false, 0
	}
	var size int64
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, size
	}
	buf := make([]byte, 8*1024)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		_, _ = file.Seek(0, io.SeekStart)
		return false, size
	}
	if n < 0 {
		n = 0
	}
	sample := buf[:n]
	maxLine := 0
	cur := 0
	lines := 1
	for _, b := range sample {
		if b == '\n' {
			if cur > maxLine {
				maxLine = cur
			}
			cur = 0
			lines++
			continue
		}
		cur++
	}
	if cur > maxLine {
		maxLine = cur
	}
	_, _ = file.Seek(0, io.SeekStart)
	if maxLine >= denseLineBytes {
		return true, size
	}
	if lines > 0 && n/lines >= denseLineBytes/2 && size >= 16*1024 {
		return true, size
	}
	return false, size
}

func compileSearchMatcher(query string, caseSensitive *bool) func(string) bool {
	explicitCase := caseSensitive != nil
	sensitive := optionalBool(caseSensitive, false)
	pattern, sensitive := searchPattern(query, sensitive, explicitCase)
	if re, err := regexp.Compile(pattern); err == nil {
		return re.MatchString
	}
	if sensitive {
		return func(line string) bool {
			return strings.Contains(line, query)
		}
	}
	needle := strings.ToLower(query)
	return func(line string) bool {
		return strings.Contains(strings.ToLower(line), needle)
	}
}

func searchPattern(query string, caseSensitive, explicitCase bool) (string, bool) {
	if isScreamingToken(query) && !explicitCase {
		caseSensitive = true
	}
	if isBareIdentifier(query) {
		escaped := regexp.QuoteMeta(query)
		// Exact symbol, or CamelCase continuation (Foo → FooBar / ContextSizes → ContextSizesForModel).
		pattern := `\b` + escaped + `(?:[A-Z][A-Za-z0-9_]*)?`
		if !hasCamelContinuation(query) {
			pattern = `\b` + escaped + `\b`
		}
		if !caseSensitive {
			return "(?i)" + pattern, caseSensitive
		}
		return pattern, caseSensitive
	}
	if !caseSensitive {
		// (?i) only applies to the next atom; wrap so a|b stays case-insensitive.
		return "(?i)(?:" + query + ")", caseSensitive
	}
	return query, caseSensitive
}

func hasCamelContinuation(query string) bool {
	if len(query) < 2 {
		return false
	}
	for _, r := range query[1:] {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func isBareIdentifier(query string) bool {
	if query == "" {
		return false
	}
	for i, r := range query {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func isScreamingToken(query string) bool {
	hasLetter := false
	for _, r := range query {
		if r >= 'A' && r <= 'Z' {
			hasLetter = true
			continue
		}
		if r == '_' || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return hasLetter
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
	used := 0
	line := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line++
		text := scanner.Text()
		if !match(text) {
			continue
		}
		clip := 300
		if len(text) > denseLineBytes {
			clip = 120
		}
		text = truncateLine(text, clip)
		item := fmt.Sprintf("%s:%d:%s", path, line, text)
		if used > 0 && used+len(item)+1 > maxSearchMatchBytes {
			matches = append(matches, "… search result truncated")
			break
		}
		matches = append(matches, item)
		used += len(item) + 1
		if len(matches) >= limit {
			break
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
	return cutAtRune(value, limit) + "…"
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
