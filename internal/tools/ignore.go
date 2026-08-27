package tools

import (
	"os"
	"regexp"
	"strings"
)

const (
	maxIgnoreFileBytes = 256 * 1024

	defaultIgnorePatterns = `.git/
node_modules/
.venv/
venv/
__pycache__/
.idea/
.cache/
.next/
`
)

type ignoreRule struct {
	dir      string
	negated  bool
	dirOnly  bool
	anchored bool
	pattern  *regexp.Regexp
}

type ignoreMatcher struct {
	rules       []ignoreRule
	hasNegation bool
}

func (r *Registry) ignoreForWalk(start string) *ignoreMatcher {
	matcher := &ignoreMatcher{}
	matcher.addFile(".", defaultIgnorePatterns)
	matcher.addFile(".", r.readIgnoreFile(".gitignore"))
	matcher.addFile(".", r.readIgnoreFile(".gxxignore"))
	if start != "" && start != "." {
		current := ""
		for _, component := range strings.Split(start, "/") {
			if component == "" || component == "." {
				continue
			}
			current = slashJoin(current, component)
			matcher.addFile(current, r.readIgnoreFile(slashJoin(current, ".gitignore")))
		}
	}
	return matcher
}

func (r *Registry) readIgnoreFile(name string) string {
	data, err := r.workspace.ReadRegularFile(name, maxIgnoreFileBytes)
	if err != nil {
		return ""
	}
	return string(data)
}

func (m *ignoreMatcher) addFile(dir, contents string) {
	dir = strings.Trim(strings.ReplaceAll(dir, "\\", "/"), "/")
	if dir == "" {
		dir = "."
	}
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimRight(line, "\r")
		if rule, ok := parseIgnoreLine(dir, line); ok {
			m.rules = append(m.rules, rule)
			if rule.negated {
				m.hasNegation = true
			}
		}
	}
}

func parseIgnoreLine(dir, line string) (ignoreRule, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ignoreRule{}, false
	}
	rule := ignoreRule{dir: dir}
	if strings.HasPrefix(line, "!") {
		rule.negated = true
		line = strings.TrimSpace(line[1:])
		if line == "" {
			return ignoreRule{}, false
		}
	}
	if strings.HasSuffix(line, "/") {
		rule.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	if strings.HasPrefix(line, "/") {
		rule.anchored = true
		line = strings.TrimPrefix(line, "/")
	} else if strings.Contains(line, "/") {
		rule.anchored = true
	}
	pattern, err := compileIgnorePattern(line)
	if err != nil {
		return ignoreRule{}, false
	}
	rule.pattern = pattern
	return rule, true
}

func compileIgnorePattern(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); {
		if pattern[i] == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i += 2
				if i < len(pattern) && pattern[i] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
				continue
			}
			b.WriteString("[^/]*")
			i++
			continue
		}
		if pattern[i] == '?' {
			b.WriteString("[^/]")
			i++
			continue
		}
		b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		i++
	}
	b.WriteByte('$')
	return regexp.Compile(b.String())
}

func compilePathGlob(glob string) (*regexp.Regexp, error) {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return nil, nil
	}
	return compileIgnorePattern(glob)
}

func matchPathGlob(pattern *regexp.Regexp, path string) bool {
	if pattern == nil {
		return true
	}
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	if path == "" {
		return false
	}
	return matchIgnore(ignoreRule{pattern: pattern}, path)
}

func hardcodedIgnored(path string, isDir bool) bool {
	parts := strings.Split(path, "/")
	for i, component := range parts {
		last := i == len(parts)-1
		if last && !isDir {
			continue
		}
		if component == ".git" {
			return true
		}
	}
	return false
}

func (m *ignoreMatcher) ignores(path string, isDir bool) bool {
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	if path == "" || path == "." {
		return false
	}
	if hardcodedIgnored(path, isDir) {
		return true
	}
	if m == nil {
		return false
	}

	ignored := false
	parts := strings.Split(path, "/")
	current := ""
	for i, part := range parts {
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		dirHere := i < len(parts)-1 || isDir
		ignored = m.apply(current, dirHere, ignored)
		if ignored && !m.hasNegation {
			return true
		}
	}
	return ignored
}

func (m *ignoreMatcher) apply(path string, isDir, ignored bool) bool {
	for _, rule := range m.rules {
		rel, ok := relativeTo(rule.dir, path)
		if !ok {
			continue
		}
		if rule.dirOnly && !isDir {
			continue
		}
		if matchIgnore(rule, rel) {
			ignored = !rule.negated
		}
	}
	return ignored
}

func relativeTo(dir, path string) (string, bool) {
	if dir == "" || dir == "." {
		return path, true
	}
	if path == dir {
		return "", true
	}
	prefix := dir + "/"
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix), true
	}
	return "", false
}

func matchIgnore(rule ignoreRule, rel string) bool {
	if rel == "" {
		return false
	}
	if rule.anchored {
		return rule.pattern.MatchString(rel)
	}
	if rule.pattern.MatchString(rel) {
		return true
	}
	for {
		slash := strings.IndexByte(rel, '/')
		if slash < 0 {
			return false
		}
		rel = rel[slash+1:]
		if rule.pattern.MatchString(rel) {
			return true
		}
	}
}

func (r *Registry) loadNestedIgnore(matcher *ignoreMatcher, current string, entry os.DirEntry, walkRoot string) {
	if matcher == nil || !entry.IsDir() || current == walkRoot || current == "." {
		return
	}
	matcher.addFile(current, r.readIgnoreFile(slashJoin(current, ".gitignore")))
}

func slashJoin(parent, child string) string {
	if parent == "" || parent == "." {
		return child
	}
	return parent + "/" + child
}
