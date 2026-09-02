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

package conversations

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gxx/internal/config"
	"gxx/internal/osutil"
)

const (
	maxPerWorkspace = 50
	maxTitleRunes   = 60
	defaultTitle    = "Untitled conversation"

	workspacePrefix = "[workspace]"
	projectPrefix   = "[project instructions from AGENTS.md"
)

// Record is a persisted conversation thread for one workspace.
type Record struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Workspace string          `json:"workspace"`
	Provider  string          `json:"provider"`
	Model     string          `json:"model"`
	Effort    string          `json:"effort"`
	Context   string          `json:"context"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	History   json.RawMessage `json:"history"`
}

// Store persists conversation records as JSON files.
type Store struct {
	dir string
}

// NewStore opens the default user conversation directory.
func NewStore() (*Store, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	return NewStoreAt(filepath.Join(filepath.Dir(path), "conversations"))
}

// NewStoreAt opens a conversation directory for tests or custom locations.
func NewStoreAt(dir string) (*Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("conversation directory cannot be empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create conversation directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save writes or updates a conversation record and prunes old entries for the workspace.
func (s *Store) Save(record Record) error {
	if s == nil {
		return errors.New("conversation store is nil")
	}
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		return errors.New("conversation id cannot be empty")
	}
	record.Workspace = filepath.Clean(record.Workspace)
	if record.Workspace == "" {
		return errors.New("conversation workspace cannot be empty")
	}
	if len(record.History) == 0 {
		return errors.New("conversation history cannot be empty")
	}
	if record.Title == "" {
		record.Title = defaultTitle
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()

	path := s.recordPath(record.ID)
	if err := writeRecordAtomic(path, record); err != nil {
		return err
	}
	return s.prune(record.Workspace)
}

func writeRecordAtomic(path string, record Record) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create conversation directory: %w", err)
	}
	if err := osutil.RestrictToCurrentUser(directory); err != nil {
		return fmt.Errorf("secure conversation directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to replace symlinked conversation")
		}
		if !info.Mode().IsRegular() {
			return errors.New("conversation path is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect conversation: %w", err)
	}

	temp, err := os.CreateTemp(directory, ".conversation-*")
	if err != nil {
		return fmt.Errorf("create temporary conversation: %w", err)
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(0o600); err != nil && osutil.UnixPermissions() {
		cleanup()
		return fmt.Errorf("secure temporary conversation: %w", err)
	}
	if err := osutil.RestrictToCurrentUser(tempPath); err != nil {
		cleanup()
		return fmt.Errorf("secure temporary conversation: %w", err)
	}
	encoder := json.NewEncoder(temp)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(record); err != nil {
		cleanup()
		return fmt.Errorf("write conversation: %w", err)
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close conversation: %w", err)
	}
	if err := osutil.ReplaceFile(tempPath, path); err != nil {
		cleanup()
		return fmt.Errorf("replace conversation: %w", err)
	}
	if err := osutil.RestrictToCurrentUser(path); err != nil {
		return fmt.Errorf("secure conversation: %w", err)
	}
	return nil
}

// List returns conversations for a workspace and provider, newest first.
func (s *Store) List(workspace, provider string) ([]Record, error) {
	if s == nil {
		return nil, errors.New("conversation store is nil")
	}
	workspace = filepath.Clean(workspace)
	provider = strings.TrimSpace(provider)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read conversation directory: %w", err)
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record, err := s.loadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		if !sameWorkspace(record.Workspace, workspace) {
			continue
		}
		if provider != "" && record.Provider != provider {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	return records, nil
}

// Load reads a conversation record by id.
func (s *Store) Load(id string) (Record, error) {
	if s == nil {
		return Record{}, errors.New("conversation store is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Record{}, errors.New("conversation id cannot be empty")
	}
	return s.loadFile(s.recordPath(id))
}

// Delete removes a conversation record.
func (s *Store) Delete(id string) error {
	if s == nil {
		return errors.New("conversation store is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("conversation id cannot be empty")
	}
	path := s.recordPath(id)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete conversation: %w", err)
	}
	return nil
}

func (s *Store) loadFile(path string) (Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return Record{}, fmt.Errorf("open conversation: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32<<20))
	if err != nil {
		return Record{}, fmt.Errorf("read conversation: %w", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, fmt.Errorf("decode conversation: %w", err)
	}
	if strings.TrimSpace(record.ID) == "" {
		return Record{}, errors.New("conversation id is missing")
	}
	return record, nil
}

func (s *Store) recordPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) prune(workspace string) error {
	records, err := s.List(workspace, "")
	if err != nil {
		return err
	}
	if len(records) <= maxPerWorkspace {
		return nil
	}
	for _, record := range records[maxPerWorkspace:] {
		if err := s.Delete(record.ID); err != nil {
			return err
		}
	}
	return nil
}

func sameWorkspace(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// NewID returns a random conversation identifier.
func NewID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("conv-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

// TitleFromHistory extracts a short title from the first user message in provider history.
func TitleFromHistory(provider string, history json.RawMessage) string {
	text := userPromptFromHistory(provider, history)
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return defaultTitle
	}
	if utf8.RuneCountInString(text) <= maxTitleRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxTitleRunes]) + "…"
}

func userPromptFromHistory(provider string, history json.RawMessage) string {
	return stripPrependedUserContext(firstUserText(provider, history))
}

func stripPrependedUserContext(text string) string {
	text = strings.TrimSpace(text)
	for text != "" {
		switch {
		case strings.HasPrefix(text, workspacePrefix):
			rest, ok := sectionAfterBlankLine(text)
			if !ok {
				return ""
			}
			text = rest
		case strings.HasPrefix(text, projectPrefix):
			rest, ok := sectionAfterBlankLine(text)
			if !ok {
				return ""
			}
			text = rest
		default:
			return text
		}
	}
	return ""
}

func sectionAfterBlankLine(text string) (string, bool) {
	idx := strings.Index(text, "\n\n")
	if idx < 0 {
		return "", false
	}
	return strings.TrimSpace(text[idx+2:]), true
}

func firstUserText(provider string, history json.RawMessage) string {
	switch strings.TrimSpace(provider) {
	case config.ProviderAnthropic:
		return firstAnthropicUserText(history)
	default:
		return firstOpenAIUserText(history)
	}
}

func firstOpenAIUserText(history json.RawMessage) string {
	var items []map[string]any
	if json.Unmarshal(history, &items) != nil {
		return ""
	}
	for _, item := range items {
		if text := openAIItemText(item); text != "" {
			return text
		}
	}
	return ""
}

func openAIItemText(item map[string]any) string {
	if role, _ := item["role"].(string); strings.EqualFold(role, "user") {
		return contentText(item["content"])
	}
	message, _ := item["message"].(map[string]any)
	if message == nil {
		return ""
	}
	if role, _ := message["role"].(string); strings.EqualFold(role, "user") {
		return contentText(message["content"])
	}
	return ""
}

func firstAnthropicUserText(history json.RawMessage) string {
	var items []map[string]any
	if json.Unmarshal(history, &items) != nil {
		return ""
	}
	for _, item := range items {
		role, _ := item["role"].(string)
		if !strings.EqualFold(role, "user") {
			continue
		}
		if text := contentText(item["content"]); text != "" {
			return text
		}
	}
	return ""
}

func contentText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		var parts []string
		for _, block := range typed {
			part, ok := block.(map[string]any)
			if !ok {
				continue
			}
			blockType, _ := part["type"].(string)
			switch blockType {
			case "text", "input_text", "output_text":
				if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}
