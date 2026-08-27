package tools

const (
	MaxToolCallsPerBatch  = maxToolCallsPerBatch
	DefaultIgnorePatterns = defaultIgnorePatterns
)

var (
	CompactDiff          = compactDiff
	SanitizedEnvironment = sanitizedEnvironment
	DefaultGoCache       = defaultGoCache
)

type IgnoreMatcher = ignoreMatcher

func NewIgnoreMatcher() *IgnoreMatcher {
	return &ignoreMatcher{}
}

func (m *ignoreMatcher) AddFile(dir, contents string) {
	m.addFile(dir, contents)
}

func (m *ignoreMatcher) Ignores(path string, isDir bool) bool {
	return m.ignores(path, isDir)
}
