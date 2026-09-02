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

package skills

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gxx/internal/osutil"
	"gxx/internal/workspace"
)

const (
	// MaxCatalog is the maximum number of skills surfaced to the model.
	MaxCatalog = 64

	maxSkillMDBytes     = 32 * 1024
	maxSkillAssetBytes  = 1 << 20
	maxNameLen          = 64
	maxDescriptionLen   = 1024
	skillFileName       = "SKILL.md"
	OriginUser          = "user"
	OriginProject       = "project"
	agentsSkillsRelRoot = ".agents/skills"
	gxxSkillsRelRoot    = ".gxx/skills"
)

// Skill is a discovered Agent Skill with metadata and a filesystem root.
type Skill struct {
	Name        string
	Description string
	Origin      string // user or project
	Root        string // absolute path to the skill directory
}

// Discover returns a stable, name-sorted catalog. Invalid skills are skipped.
// Name collisions resolve as .gxx/skills > .agents/skills > user skills.
func Discover(ws *workspace.Workspace, userDir string) []Skill {
	byName := make(map[string]Skill)

	for _, skill := range discoverUser(userDir) {
		byName[skill.Name] = skill
	}
	if ws != nil {
		for _, skill := range discoverProject(ws, agentsSkillsRelRoot) {
			byName[skill.Name] = skill
		}
		for _, skill := range discoverProject(ws, gxxSkillsRelRoot) {
			byName[skill.Name] = skill
		}
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > MaxCatalog {
		names = names[:MaxCatalog]
	}
	out := make([]Skill, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

// Lookup finds a skill by name in catalog, or false if missing.
func Lookup(catalog []Skill, name string) (Skill, bool) {
	name = strings.TrimSpace(name)
	for _, skill := range catalog {
		if skill.Name == name {
			return skill, true
		}
	}
	return Skill{}, false
}

// Read loads a file relative to the skill root. Empty relPath defaults to SKILL.md.
// For SKILL.md the markdown body is returned without frontmatter; other files are raw.
func Read(skill Skill, relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		relPath = skillFileName
	}
	clean, err := cleanSkillPath(relPath)
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(skill.Root)
	if root == "" {
		return "", errors.New("skill root is empty")
	}
	guard, err := os.OpenRoot(root)
	if err != nil {
		return "", fmt.Errorf("open skill root: %w", err)
	}
	defer guard.Close()

	if err := rejectSymlinkComponents(guard, clean, false); err != nil {
		return "", err
	}

	limit := int64(maxSkillAssetBytes)
	if clean == skillFileName {
		limit = maxSkillMDBytes
	}
	data, err := readRegularThroughRoot(guard, clean, limit)
	if err != nil {
		return "", err
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return "", fmt.Errorf("refusing to read binary file: %s", relPath)
	}
	if clean == skillFileName {
		_, _, body, err := parseSkillMD(data, skill.Name)
		if err != nil {
			return "", err
		}
		return body, nil
	}
	return string(data), nil
}

func discoverUser(userDir string) []Skill {
	userDir = strings.TrimSpace(userDir)
	if userDir == "" {
		return nil
	}
	info, err := os.Lstat(userDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	entries, err := os.ReadDir(userDir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
			continue
		}
		skillRoot := filepath.Join(userDir, name)
		info, err := os.Lstat(skillRoot)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if skill, ok := loadSkill(skillRoot, name, OriginUser); ok {
			out = append(out, skill)
		}
	}
	return out
}

func discoverProject(ws *workspace.Workspace, relRoot string) []Skill {
	if ws == nil {
		return nil
	}
	entries, err := fs.ReadDir(ws.FS(), relRoot)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
			continue
		}
		relDir := relRoot + "/" + name
		info, err := ws.Lstat(relDir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		skillRoot := filepath.Join(ws.Root(), filepath.FromSlash(relDir))
		if skill, ok := loadSkill(skillRoot, name, OriginProject); ok {
			out = append(out, skill)
		}
	}
	return out
}

func loadSkill(root, dirName, origin string) (Skill, bool) {
	guard, err := os.OpenRoot(root)
	if err != nil {
		return Skill{}, false
	}
	defer guard.Close()
	if err := rejectSymlinkComponents(guard, skillFileName, false); err != nil {
		return Skill{}, false
	}
	data, err := readRegularThroughRoot(guard, skillFileName, maxSkillMDBytes)
	if err != nil {
		return Skill{}, false
	}
	name, description, _, err := parseSkillMD(data, dirName)
	if err != nil {
		return Skill{}, false
	}
	return Skill{
		Name:        name,
		Description: description,
		Origin:      origin,
		Root:        root,
	}, true
}

func cleanSkillPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path cannot be empty")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("absolute paths are not allowed")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path leaves the skill root")
	}
	return filepath.ToSlash(clean), nil
}

func rejectSymlinkComponents(guard *os.Root, path string, allowMissing bool) error {
	if path == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == "" || component == "." {
			continue
		}
		if current == "" {
			current = component
		} else {
			current += "/" + component
		}
		info, err := guard.Lstat(current)
		if err != nil {
			if allowMissing && errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return workspace.Describe(current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing path with symlink component: %s", current)
		}
	}
	return nil
}

func readRegularThroughRoot(guard *os.Root, path string, maxBytes int64) ([]byte, error) {
	file, err := guard.OpenFile(path, osutil.ReadNoFollowFlags(), 0)
	if err != nil {
		return nil, workspace.Describe(path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, workspace.Describe(path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes: %s", maxBytes, path)
	}
	reader := io.Reader(file)
	if maxBytes > 0 {
		reader = io.LimitReader(file, maxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes: %s", maxBytes, path)
	}
	return data, nil
}
