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
	DefaultEffort             = "medium"
	DefaultContext            = "272k"
	PermissionAsk             = "ask"
	PermissionAutoWrites      = "auto-writes"
	PermissionAuto            = "auto"
	DefaultPermissionMode     = PermissionAsk
	DefaultMaxSteps           = 40
	DefaultMaxToolResultBytes = 64 * 1024
	DefaultMaxSearchResults   = 100
	DefaultParallelReads      = 4
	maxConfigBytes            = 64 * 1024
)

var PermissionModes = []string{PermissionAsk, PermissionAutoWrites, PermissionAuto}

var (
	DefaultCommandTimeout = 2 * time.Minute
	DefaultAPITimeout     = 10 * time.Minute
)

// Config contains the runtime settings shared by the CLI, model, and tools.
type Config struct {
	APIKey             string
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
}

// Load reads environment configuration and applies conservative defaults.
func Load(workspace string) Config {
	stored, _ := loadPersistent()
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(stored.OpenAIAPIKey)
	}
	return Config{
		APIKey:             apiKey,
		Model:              envString("GXX_MODEL", firstNonEmpty(stored.Model, DefaultModel)),
		Effort:             envString("GXX_EFFORT", validEffortOr(stored.Effort, DefaultEffort)),
		Context:            envString("GXX_CONTEXT", validContextOr(stored.Context, DefaultContext)),
		Fast:               envBool("GXX_FAST", stored.Fast),
		PermissionMode:     envString("GXX_PERMISSION", validPermissionOr(stored.Permission, DefaultPermissionMode)),
		Workspace:          workspace,
		MaxSteps:           envInt("GXX_MAX_STEPS", DefaultMaxSteps),
		MaxToolResultBytes: envInt("GXX_MAX_TOOL_RESULT_BYTES", DefaultMaxToolResultBytes),
		MaxSearchResults:   envInt("GXX_MAX_SEARCH_RESULTS", DefaultMaxSearchResults),
		ParallelReads:      envInt("GXX_PARALLEL_READS", DefaultParallelReads),
		CommandTimeout:     envDuration("GXX_COMMAND_TIMEOUT", DefaultCommandTimeout),
		APITimeout:         envDuration("GXX_API_TIMEOUT", DefaultAPITimeout),
	}
}

// Validate normalizes the workspace and rejects unusable settings.
func (c *Config) Validate() error {
	return c.validate(true)
}

// ValidateInteractive permits an empty API key so the REPL can open /config.
func (c *Config) ValidateInteractive() error {
	return c.validate(false)
}

func (c *Config) validate(requireAPIKey bool) error {
	if requireAPIKey && strings.TrimSpace(c.APIKey) == "" {
		return errors.New("OPENAI_API_KEY is not set")
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("model cannot be empty")
	}
	c.Model = CanonicalModel(c.Model)
	if err := ValidateEffort(c.Effort); err != nil {
		return err
	}
	normalized, err := NormalizeContext(c.Context)
	if err != nil {
		return err
	}
	c.Context = normalized
	permission, err := CanonicalPermission(c.PermissionMode)
	if err != nil {
		return err
	}
	c.PermissionMode = permission
	if c.MaxSteps < 1 {
		return errors.New("max steps must be at least 1")
	}
	if c.MaxToolResultBytes < 1024 {
		return errors.New("max tool result bytes must be at least 1024")
	}
	if c.MaxSearchResults < 1 {
		return errors.New("max search results must be at least 1")
	}
	if c.ParallelReads < 1 {
		return errors.New("parallel reads must be at least 1")
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
	OpenAIAPIKey string `json:"openai_api_key,omitempty"`
	Model        string `json:"model,omitempty"`
	Effort       string `json:"effort,omitempty"`
	Context      string `json:"context,omitempty"`
	Fast         bool   `json:"fast,omitempty"`
	Permission   string `json:"permission,omitempty"`
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
		return nil
	})
}

// SaveSession persists REPL model settings without touching the API key.
func SaveSession(model, effort, contextValue string, fast bool, permission string) (string, error) {
	return savePersistent(func(stored *persistentConfig) error {
		stored.Model = strings.TrimSpace(model)
		stored.Effort = strings.TrimSpace(effort)
		stored.Context = strings.TrimSpace(contextValue)
		stored.Fast = fast
		stored.Permission = strings.TrimSpace(permission)
		return nil
	})
}

func loadPersistent() (persistentConfig, error) {
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
	if osutil.UnixPermissions() && info.Mode().Perm()&0o077 != 0 {
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
	stored, err := loadPersistent()
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

var ContextSizes = []string{"32k", "128k", "272k", "1m"}

var contextTokens = map[string]int{
	"32k":  32_000,
	"128k": 128_000,
	"272k": 272_000,
	"1m":   1_050_000,
}

// CanonicalModel maps GPT-5.6 aliases onto sol, terra, or luna.
func CanonicalModel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sol", "gpt-5.6-sol", "gpt-5.6":
		return "gpt-5.6-sol"
	case "terra", "gpt-5.6-terra":
		return "gpt-5.6-terra"
	case "luna", "gpt-5.6-luna":
		return "gpt-5.6-luna"
	default:
		return strings.TrimSpace(value)
	}
}

func NormalizeContext(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "32k", "32000":
		return "32k", nil
	case "128k", "128000":
		return "128k", nil
	case "272k", "272000":
		return "272k", nil
	case "1m", "1050k", "1.05m", "1050000":
		return "1m", nil
	case "":
		return "", errors.New("context cannot be empty")
	default:
		return "", fmt.Errorf("context must be one of %s", strings.Join(ContextSizes, ", "))
	}
}

func ContextTokens(value string) int {
	label, err := NormalizeContext(value)
	if err != nil {
		return contextTokens[DefaultContext]
	}
	return contextTokens[label]
}
