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

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gxx/internal/osutil"
)

const (
	DefaultModel              = "gpt-5.6-sol"
	DefaultClaudeModel        = "claude-sonnet-5"
	AccountOpenAI             = "openai"
	AccountClaude             = "claude"
	AccountAPI                = "api"
	DefaultEffort             = "medium"
	DefaultContext            = "272k"
	ProviderOpenAI            = "openai"
	ProviderAnthropic         = "anthropic"
	DefaultProvider           = ProviderOpenAI
	PermissionAsk             = "ask"
	PermissionAutoWrites      = "auto-writes"
	PermissionAuto            = "auto"
	DefaultPermissionMode     = PermissionAsk
	DefaultMaxSteps           = 24
	DefaultMaxToolResultBytes = 64 * 1024
	DefaultMaxSearchResults   = 100
	DefaultParallelReads      = 4
	MaxStepsLimit             = 200
	MaxToolResultBytesLimit   = 1 << 20
	MaxSearchResultsLimit     = 1000
	ParallelReadsLimit        = 32
	maxConfigBytes            = 64 * 1024
)

var PermissionModes = []string{PermissionAsk, PermissionAutoWrites, PermissionAuto}

var Providers = []string{ProviderOpenAI, ProviderAnthropic}

var (
	DefaultCommandTimeout = 2 * time.Minute
	DefaultAPITimeout     = 10 * time.Minute
)

// ClaudeTokens is a Claude subscription OAuth credential set.
type ClaudeTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// OpenAITokens is a ChatGPT Codex OAuth credential set.
type OpenAITokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	AccountID    string
}

// Config contains the runtime settings shared by the CLI, model, and tools.
type Config struct {
	APIKey             string
	Provider           string
	ClaudeTokens       ClaudeTokens
	OpenAITokens       OpenAITokens
	Model              string
	Effort             string
	Context            string
	Fast               bool
	PermissionMode     string
	Workspace          string
	MaxSteps           int
	MaxToolResultBytes int
	MaxSearchResults   int
	ParallelReads      int
	CommandTimeout     time.Duration
	APITimeout         time.Duration
	LoadError          error
}

// Load reads environment configuration and applies conservative defaults.
func Load(workspace string) Config {
	stored, loadErr := loadPersistent()
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(stored.OpenAIAPIKey)
	}
	model := envString("GXX_MODEL", firstNonEmpty(stored.Model, DefaultModel))
	providerHint := envString("GXX_PROVIDER", stored.Provider)
	if strings.TrimSpace(os.Getenv("GXX_MODEL")) == "" {
		if hint, err := CanonicalProvider(providerHint); err == nil && hint == ProviderAnthropic && !IsClaudeModel(model) {
			model = DefaultClaudeModel
		}
	}
	cfg := Config{
		APIKey:             apiKey,
		Provider:           resolveProvider(providerHint, model),
		ClaudeTokens:       resolveClaudeTokens(stored),
		OpenAITokens:       stored.openAITokens(),
		Model:              model,
		Effort:             envString("GXX_EFFORT", validEffortOr(stored.Effort, DefaultEffort)),
		Context:            envString("GXX_CONTEXT", validContextOr(stored.Context, DefaultContextForModel(model))),
		Fast:               envBool("GXX_FAST", stored.Fast),
		PermissionMode:     envString("GXX_PERMISSION", validPermissionOr(stored.Permission, DefaultPermissionMode)),
		Workspace:          workspace,
		MaxSteps:           envIntRange("GXX_MAX_STEPS", DefaultMaxSteps, 1, MaxStepsLimit),
		MaxToolResultBytes: envIntRange("GXX_MAX_TOOL_RESULT_BYTES", DefaultMaxToolResultBytes, 1024, MaxToolResultBytesLimit),
		MaxSearchResults:   envIntRange("GXX_MAX_SEARCH_RESULTS", DefaultMaxSearchResults, 1, MaxSearchResultsLimit),
		ParallelReads:      envIntRange("GXX_PARALLEL_READS", DefaultParallelReads, 1, ParallelReadsLimit),
		CommandTimeout:     envDuration("GXX_COMMAND_TIMEOUT", DefaultCommandTimeout),
		APITimeout:         envDuration("GXX_API_TIMEOUT", DefaultAPITimeout),
		LoadError:          loadErr,
	}
	cfg.Context = ClampContextForModel(cfg.Model, cfg.Context)
	return cfg
}

// Validate normalizes the workspace and rejects unusable settings.
func (c *Config) Validate() error {
	return c.validate(true)
}

// ValidateInteractive permits an empty API key so the REPL can open /config.
func (c *Config) ValidateInteractive() error {
	return c.validate(false)
}

func (c *Config) validate(requireCredentials bool) error {
	if c.LoadError != nil {
		return fmt.Errorf("read config: %w", c.LoadError)
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("model cannot be empty")
	}
	c.Model = CanonicalModel(c.Model)
	c.Provider = resolveProvider(c.Provider, c.Model)
	if requireCredentials {
		if err := c.MissingCredentialError(); err != nil {
			return err
		}
	}
	if err := ValidateEffort(c.Effort); err != nil {
		return err
	}
	normalized, err := NormalizeContext(c.Context)
	if err != nil {
		if isRemovedContextLabel(c.Context) {
			c.Context = DefaultContextForModel(c.Model)
		} else {
			return err
		}
	} else {
		c.Context = ClampContextForModel(c.Model, normalized)
	}
	permission, err := CanonicalPermission(c.PermissionMode)
	if err != nil {
		return err
	}
	c.PermissionMode = permission
	if c.MaxSteps < 1 {
		return errors.New("max steps must be at least 1")
	}
	if c.MaxSteps > MaxStepsLimit {
		c.MaxSteps = MaxStepsLimit
	}
	if c.MaxToolResultBytes < 1024 {
		return errors.New("max tool result bytes must be at least 1024")
	}
	if c.MaxToolResultBytes > MaxToolResultBytesLimit {
		c.MaxToolResultBytes = MaxToolResultBytesLimit
	}
	if c.MaxSearchResults < 1 {
		return errors.New("max search results must be at least 1")
	}
	if c.MaxSearchResults > MaxSearchResultsLimit {
		c.MaxSearchResults = MaxSearchResultsLimit
	}
	if c.ParallelReads < 1 {
		return errors.New("parallel reads must be at least 1")
	}
	if c.ParallelReads > ParallelReadsLimit {
		c.ParallelReads = ParallelReadsLimit
	}
	if c.CommandTimeout <= 0 {
		return errors.New("command timeout must be positive")
	}
	if c.APITimeout <= 0 {
		return errors.New("API timeout must be positive")
	}

	root, err := filepath.Abs(c.Workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace is not a directory: %s", root)
	}
	c.Workspace = filepath.Clean(root)
	return nil
}

type persistentConfig struct {
	OpenAIAPIKey       string `json:"openai_api_key,omitempty"`
	Provider           string `json:"provider,omitempty"`
	ClaudeAccessToken  string `json:"claude_access_token,omitempty"`
	ClaudeRefreshToken string `json:"claude_refresh_token,omitempty"`
	ClaudeExpiresAt    string `json:"claude_expires_at,omitempty"`
	OpenAIAccessToken  string `json:"openai_access_token,omitempty"`
	OpenAIRefreshToken string `json:"openai_refresh_token,omitempty"`
	OpenAIExpiresAt    string `json:"openai_expires_at,omitempty"`
	OpenAIAccountID    string `json:"openai_account_id,omitempty"`
	Model              string `json:"model,omitempty"`
	Effort             string `json:"effort,omitempty"`
	Context            string `json:"context,omitempty"`
	Fast               bool   `json:"fast,omitempty"`
	Permission         string `json:"permission,omitempty"`
}

// Path returns the preferred gxx config file. XDG_CONFIG_HOME wins on every
// OS. Otherwise Windows uses %APPDATA%\gxx; Unix uses ~/.config/gxx.
func Path() (string, error) {
	if base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); base != "" {
		if !filepath.IsAbs(base) {
			return "", errors.New("XDG_CONFIG_HOME must be an absolute path")
		}
		return filepath.Join(base, "gxx", "config.json"), nil
	}
	if runtime.GOOS == "windows" {
		return windowsConfigPath()
	}
	return unixConfigPath()
}

// UserSkillsDir returns the personal Agent Skills directory beside config.json
// (~/.config/gxx/skills, or %APPDATA%\gxx\skills on Windows).
func UserSkillsDir() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "skills"), nil
}

func windowsConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("APPDATA is not set")
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("APPDATA must be an absolute path")
	}
	return filepath.Join(base, "gxx", "config.json"), nil
}

func unixConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "gxx", "config.json"), nil
}

// existingConfigPath prefers Path(). On Windows, a leftover ~/.config/gxx
// file from earlier builds is still read until the next save migrates it.
func existingConfigPath() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	exists, err := configExists(path)
	if err != nil {
		return "", err
	}
	if exists || runtime.GOOS != "windows" || strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")) != "" {
		return path, nil
	}
	legacy, err := unixConfigPath()
	if err != nil {
		return path, nil
	}
	if configPathsEqual(path, legacy) {
		return path, nil
	}
	exists, err = configExists(legacy)
	if err != nil {
		return "", err
	}
	if exists {
		return legacy, nil
	}
	return path, nil
}

func configExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect config: %w", err)
}

func configPathsEqual(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// LoadAPIKey reads a private API key from the persistent config, if present.
func LoadAPIKey() (string, error) {
	stored, err := loadPersistent()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stored.OpenAIAPIKey), nil
}

// SaveAPIKey atomically stores the API key in a user-private config file.
func SaveAPIKey(apiKey string) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", errors.New("API key cannot be empty")
	}
	return savePersistent(func(stored *persistentConfig) error {
		stored.OpenAIAPIKey = apiKey
		clearOpenAITokens(stored)
		clearClaudeTokens(stored)
		return nil
	})
}

// ClearAPIKey removes the persisted platform API key.
func ClearAPIKey() (string, error) {
	return savePersistent(func(stored *persistentConfig) error {
		stored.OpenAIAPIKey = ""
		return nil
	})
}

// SaveSession persists REPL model settings without touching credentials.
func SaveSession(model, effort, contextValue string, fast bool, permission string) (string, error) {
	return savePersistent(func(stored *persistentConfig) error {
		stored.Model = strings.TrimSpace(model)
		stored.Provider = ProviderForModel(model)
		stored.Effort = strings.TrimSpace(effort)
		stored.Context = strings.TrimSpace(contextValue)
		stored.Fast = fast
		stored.Permission = strings.TrimSpace(permission)
		return nil
	})
}

// LoadClaudeTokens reads persisted Claude OAuth tokens, if present.
func LoadClaudeTokens() (ClaudeTokens, error) {
	stored, err := loadPersistent()
	if err != nil {
		return ClaudeTokens{}, err
	}
	return stored.claudeTokens(), nil
}

// SaveClaudeTokens atomically stores Claude OAuth tokens.
func SaveClaudeTokens(tokens ClaudeTokens) (string, error) {
	tokens.AccessToken = strings.TrimSpace(tokens.AccessToken)
	tokens.RefreshToken = strings.TrimSpace(tokens.RefreshToken)
	if tokens.AccessToken == "" {
		return "", errors.New("Claude access token cannot be empty")
	}
	return savePersistent(func(stored *persistentConfig) error {
		stored.ClaudeAccessToken = tokens.AccessToken
		stored.ClaudeRefreshToken = tokens.RefreshToken
		if tokens.ExpiresAt.IsZero() {
			stored.ClaudeExpiresAt = ""
		} else {
			stored.ClaudeExpiresAt = tokens.ExpiresAt.UTC().Format(time.RFC3339)
		}
		stored.OpenAIAPIKey = ""
		clearOpenAITokens(stored)
		return nil
	})
}

// ClearClaudeTokens removes persisted Claude OAuth tokens.
func ClearClaudeTokens() (string, error) {
	return savePersistent(func(stored *persistentConfig) error {
		clearClaudeTokens(stored)
		return nil
	})
}

// LoadOpenAITokens reads persisted ChatGPT Codex OAuth tokens, if present.
func LoadOpenAITokens() (OpenAITokens, error) {
	stored, err := loadPersistent()
	if err != nil {
		return OpenAITokens{}, err
	}
	return stored.openAITokens(), nil
}

// SaveOpenAITokens atomically stores ChatGPT Codex OAuth tokens.
func SaveOpenAITokens(tokens OpenAITokens) (string, error) {
	tokens.AccessToken = strings.TrimSpace(tokens.AccessToken)
	tokens.RefreshToken = strings.TrimSpace(tokens.RefreshToken)
	tokens.AccountID = strings.TrimSpace(tokens.AccountID)
	if tokens.AccessToken == "" {
		return "", errors.New("OpenAI access token cannot be empty")
	}
	return savePersistent(func(stored *persistentConfig) error {
		stored.OpenAIAccessToken = tokens.AccessToken
		stored.OpenAIRefreshToken = tokens.RefreshToken
		stored.OpenAIAccountID = tokens.AccountID
		if tokens.ExpiresAt.IsZero() {
			stored.OpenAIExpiresAt = ""
		} else {
			stored.OpenAIExpiresAt = tokens.ExpiresAt.UTC().Format(time.RFC3339)
		}
		stored.OpenAIAPIKey = ""
		clearClaudeTokens(stored)
		return nil
	})
}

// ClearOpenAITokens removes persisted ChatGPT Codex OAuth tokens.
func ClearOpenAITokens() (string, error) {
	return savePersistent(func(stored *persistentConfig) error {
		clearOpenAITokens(stored)
		return nil
	})
}

func clearClaudeTokens(stored *persistentConfig) {
	stored.ClaudeAccessToken = ""
	stored.ClaudeRefreshToken = ""
	stored.ClaudeExpiresAt = ""
}

func clearOpenAITokens(stored *persistentConfig) {
	stored.OpenAIAccessToken = ""
	stored.OpenAIRefreshToken = ""
	stored.OpenAIExpiresAt = ""
	stored.OpenAIAccountID = ""
}

// HasOpenAICredentials reports whether a platform API key or Codex OAuth token is available.
func (c Config) HasOpenAICredentials() bool {
	return strings.TrimSpace(c.APIKey) != "" || strings.TrimSpace(c.OpenAITokens.AccessToken) != ""
}

// HasOpenAIAPIKey reports whether a platform API key is available.
func (c Config) HasOpenAIAPIKey() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

// HasClaudeCredentials reports whether a Claude OAuth access token is available.
func (c Config) HasClaudeCredentials() bool {
	return strings.TrimSpace(c.ClaudeTokens.AccessToken) != ""
}

// ActiveAccount is the connected login that matches the current model when
// more than one credential is present: openai, claude, api, or empty.
func (c Config) ActiveAccount() string {
	claude := c.HasClaudeCredentials()
	api := c.HasOpenAIAPIKey()
	openai := strings.TrimSpace(c.OpenAITokens.AccessToken) != ""
	if IsClaudeModel(c.Model) && claude {
		return AccountClaude
	}
	if api {
		return AccountAPI
	}
	if openai {
		return AccountOpenAI
	}
	if claude {
		return AccountClaude
	}
	return ""
}

// MissingCredentialError returns a provider-specific missing-credential error.
func (c Config) MissingCredentialError() error {
	switch c.Provider {
	case ProviderAnthropic:
		if !c.HasClaudeCredentials() {
			return errors.New("Claude is not logged in; run gxx login")
		}
	default:
		if !c.HasOpenAICredentials() {
			return errors.New("OpenAI is not configured; run gxx login")
		}
	}
	return nil
}

func resolveClaudeTokens(stored persistentConfig) ClaudeTokens {
	if token := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); token != "" {
		return ClaudeTokens{AccessToken: token}
	}
	return stored.claudeTokens()
}

func (stored persistentConfig) claudeTokens() ClaudeTokens {
	tokens := ClaudeTokens{
		AccessToken:  strings.TrimSpace(stored.ClaudeAccessToken),
		RefreshToken: strings.TrimSpace(stored.ClaudeRefreshToken),
	}
	if expires := strings.TrimSpace(stored.ClaudeExpiresAt); expires != "" {
		if parsed, err := time.Parse(time.RFC3339, expires); err == nil {
			tokens.ExpiresAt = parsed
		}
	}
	return tokens
}

func (stored persistentConfig) openAITokens() OpenAITokens {
	tokens := OpenAITokens{
		AccessToken:  strings.TrimSpace(stored.OpenAIAccessToken),
		RefreshToken: strings.TrimSpace(stored.OpenAIRefreshToken),
		AccountID:    strings.TrimSpace(stored.OpenAIAccountID),
	}
	if expires := strings.TrimSpace(stored.OpenAIExpiresAt); expires != "" {
		if parsed, err := time.Parse(time.RFC3339, expires); err == nil {
			tokens.ExpiresAt = parsed
		}
	}
	return tokens
}

func resolveProvider(hint, model string) string {
	if IsClaudeModel(model) {
		return ProviderAnthropic
	}
	if IsOpenAIModel(model) {
		return ProviderOpenAI
	}
	if provider, err := CanonicalProvider(hint); err == nil {
		return provider
	}
	return DefaultProvider
}

func loadPersistent() (persistentConfig, error) {
	return readPersistent(true)
}

// loadPersistentForSave reads config so a later write can preserve fields.
// Invalid JSON is treated as empty so login can recreate the file. Symlinks
// are still refused. World-readable files are read so a save can restore 0600
// without dropping the key.
func loadPersistentForSave() (persistentConfig, error) {
	stored, err := readPersistent(false)
	if err == nil {
		return stored, nil
	}
	if strings.Contains(err.Error(), "decode config") {
		return persistentConfig{}, nil
	}
	return persistentConfig{}, err
}

func readPersistent(requireSecure bool) (persistentConfig, error) {
	var stored persistentConfig
	path, err := existingConfigPath()
	if err != nil {
		return stored, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return stored, errors.New("config is a symlink")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		return stored, nil
	} else {
		return stored, fmt.Errorf("inspect config: %w", err)
	}
	file, err := os.OpenFile(path, osutil.ReadNoFollowFlags(), 0)
	if errors.Is(err, os.ErrNotExist) {
		return stored, nil
	}
	if err != nil {
		return stored, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return stored, fmt.Errorf("stat config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return stored, errors.New("config is not a regular file")
	}
	if requireSecure && osutil.UnixPermissions() && info.Mode().Perm()&0o077 != 0 {
		return stored, fmt.Errorf("config permissions are %04o; expected 0600", info.Mode().Perm())
	}

	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return stored, fmt.Errorf("read config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return stored, fmt.Errorf("config exceeds %d bytes", maxConfigBytes)
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return persistentConfig{}, fmt.Errorf("decode config: %w", err)
	}
	return stored, nil
}

func savePersistent(mutate func(*persistentConfig) error) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	stored, err := loadPersistentForSave()
	if err != nil {
		return "", err
	}
	if err := mutate(&stored); err != nil {
		return "", err
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	if err := osutil.RestrictToCurrentUser(directory); err != nil {
		return "", fmt.Errorf("secure config directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("refusing to replace symlinked config")
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("config path is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect config: %w", err)
	}

	temp, err := os.CreateTemp(directory, ".config-*")
	if err != nil {
		return "", fmt.Errorf("create temporary config: %w", err)
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(0o600); err != nil && osutil.UnixPermissions() {
		cleanup()
		return "", fmt.Errorf("secure temporary config: %w", err)
	}
	if err := osutil.RestrictToCurrentUser(tempPath); err != nil {
		cleanup()
		return "", fmt.Errorf("secure temporary config: %w", err)
	}
	encoder := json.NewEncoder(temp)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(stored); err != nil {
		cleanup()
		return "", fmt.Errorf("write config: %w", err)
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("close config: %w", err)
	}
	if err := osutil.ReplaceFile(tempPath, path); err != nil {
		cleanup()
		return "", fmt.Errorf("replace config: %w", err)
	}
	if err := osutil.RestrictToCurrentUser(path); err != nil {
		return "", fmt.Errorf("secure config: %w", err)
	}
	removeLegacyWindowsConfig(path)
	return path, nil
}

func removeLegacyWindowsConfig(preferred string) {
	if runtime.GOOS != "windows" || strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")) != "" {
		return
	}
	legacy, err := unixConfigPath()
	if err != nil || configPathsEqual(preferred, legacy) {
		return
	}
	_ = os.Remove(legacy)
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envIntRange(name string, fallback, min, max int) int {
	parsed := envInt(name, fallback)
	if parsed < min {
		return fallback
	}
	if max >= min && parsed > max {
		return max
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	default:
		return fallback
	}
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func validEffortOr(value, fallback string) string {
	if err := ValidateEffort(value); err != nil {
		return fallback
	}
	return value
}

func validContextOr(value, fallback string) string {
	normalized, err := NormalizeContext(value)
	if err != nil {
		return fallback
	}
	return normalized
}

func validPermissionOr(value, fallback string) string {
	canonical, err := CanonicalPermission(value)
	if err != nil {
		return fallback
	}
	return canonical
}

func CanonicalPermission(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PermissionAsk:
		return PermissionAsk, nil
	case PermissionAutoWrites, "auto-write", "autowrites":
		return PermissionAutoWrites, nil
	case PermissionAuto, "yolo":
		return PermissionAuto, nil
	case "":
		return "", errors.New("permission mode cannot be empty")
	default:
		return "", fmt.Errorf(
			"permission mode must be one of %s",
			strings.Join(PermissionModes, ", "),
		)
	}
}

func ValidateEffort(value string) error {
	switch strings.TrimSpace(value) {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return nil
	case "":
		return errors.New("effort cannot be empty")
	default:
		return fmt.Errorf(
			"effort must be one of none, minimal, low, medium, high, xhigh, max",
		)
	}
}

// CanonicalModel maps provider aliases onto bundled model IDs.
func CanonicalModel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sol", "gpt-5.6-sol", "gpt-5.6":
		return "gpt-5.6-sol"
	case "terra", "gpt-5.6-terra":
		return "gpt-5.6-terra"
	case "luna", "gpt-5.6-luna":
		return "gpt-5.6-luna"
	case "fable", "claude-fable", "claude-fable-5":
		return "claude-fable-5"
	case "mythos", "claude-mythos", "claude-mythos-5", "claude-mythos-preview":
		return "claude-mythos-5"
	case "opus", "claude-opus", "claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-opus-4":
		return "claude-opus-5"
	case "sonnet", "claude-sonnet", "claude-sonnet-5", "claude-sonnet-4-6", "claude-sonnet-4", "claude":
		return "claude-sonnet-5"
	case "haiku", "claude-haiku", "claude-haiku-4-5", "claude-haiku-4":
		return "claude-haiku-4-5"
	default:
		return strings.TrimSpace(value)
	}
}

// IsClaudeModel reports whether the model belongs to the Anthropic family.
func IsClaudeModel(value string) bool {
	return strings.HasPrefix(strings.ToLower(CanonicalModel(value)), "claude-")
}

// IsOpenAIModel reports whether the model belongs to the OpenAI family.
func IsOpenAIModel(value string) bool {
	return strings.HasPrefix(strings.ToLower(CanonicalModel(value)), "gpt-")
}

// ProviderForModel returns the backend implied by a model ID.
func ProviderForModel(value string) string {
	if IsClaudeModel(value) {
		return ProviderAnthropic
	}
	return ProviderOpenAI
}

// CanonicalProvider maps provider aliases onto openai or anthropic.
func CanonicalProvider(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProviderOpenAI, "gpt", "oai":
		return ProviderOpenAI, nil
	case ProviderAnthropic, "claude":
		return ProviderAnthropic, nil
	case "":
		return "", errors.New("provider cannot be empty")
	default:
		return "", fmt.Errorf("provider must be one of %s", strings.Join(Providers, ", "))
	}
}
