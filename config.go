package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultDBPath = "~/.pegg/logs.db"

type Settings struct {
	Enabled               bool   `json:"enabled"`
	DBPath                string `json:"db_path"`
	DefaultRangeDays      int    `json:"default_range_days"`
	DefaultLimit          int    `json:"default_limit"`
	RequestTimeoutSeconds int    `json:"request_timeout_seconds"`

	Workdir    string `json:"-"`
	PluginPath string `json:"-"`
	Token      string `json:"-"`
}

func defaultSettings() Settings {
	wd, _ := os.Getwd()
	return Settings{
		Enabled:               true,
		DBPath:                defaultDBPath,
		DefaultRangeDays:      7,
		DefaultLimit:          100,
		RequestTimeoutSeconds: 30,
		Workdir:               wd,
	}
}

func (s *Settings) applyHost(configJSON []byte) {
	if len(configJSON) == 0 {
		return
	}
	var host struct {
		Plugins struct {
			Options map[string]json.RawMessage `json:"options"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(configJSON, &host); err != nil {
		return
	}
	for _, key := range []string{pluginName, "pegg-" + pluginName} {
		raw := host.Plugins.Options[key]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		_ = json.Unmarshal(raw, s)
	}
}

func (s *Settings) applyOptions(options json.RawMessage) error {
	if len(options) == 0 || string(options) == "null" {
		return nil
	}
	if err := json.Unmarshal(options, s); err != nil {
		return fmt.Errorf("parse %s options: %w", pluginName, err)
	}
	return nil
}

func (s *Settings) applyEnv() {
	if v := strings.TrimSpace(os.Getenv("PEGG_LOGS_DB")); v != "" {
		s.DBPath = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_LOGS_RANGE_DAYS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.DefaultRangeDays = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_LOGS_LIMIT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.DefaultLimit = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_LOGS_TIMEOUT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.RequestTimeoutSeconds = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_LOGS_DISABLED")); v == "1" || strings.EqualFold(v, "true") {
		s.Enabled = false
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_LOGS_DASHBOARD_TOKEN")); v != "" {
		s.Token = v
	}
	s.normalize()
}

func (s *Settings) normalize() {
	s.DBPath = strings.TrimSpace(s.DBPath)
	if s.DBPath == "" {
		s.DBPath = defaultDBPath
	}
	s.DBPath = expandHome(s.DBPath)
	if s.DefaultRangeDays <= 0 {
		s.DefaultRangeDays = 7
	}
	if s.DefaultLimit <= 0 {
		s.DefaultLimit = 100
	}
	if s.DefaultLimit > 1000 {
		s.DefaultLimit = 1000
	}
	if s.RequestTimeoutSeconds <= 0 {
		s.RequestTimeoutSeconds = 30
	}
	if strings.TrimSpace(s.Workdir) == "" {
		s.Workdir, _ = os.Getwd()
	}
	s.Workdir = expandHome(s.Workdir)
	s.Token = strings.TrimSpace(s.Token)
}

func (s Settings) timeout() time.Duration {
	return time.Duration(s.RequestTimeoutSeconds) * time.Second
}

func (s Settings) dbPath() string {
	path := expandHome(strings.TrimSpace(s.DBPath))
	if path == "" {
		path = expandHome(defaultDBPath)
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func (s Settings) stateDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".pegg", "logs")
	}
	return filepath.Join(s.Workdir, ".pegg", "logs")
}

func (s Settings) serverPath() string {
	return filepath.Join(s.stateDir(), "server.json")
}

func (s Settings) serverLogPath() string {
	return filepath.Join(s.stateDir(), "server.log")
}

func (s Settings) exportDir() string {
	return filepath.Join(s.Workdir, ".pegg", "logs")
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func redactSettings(msg string, s Settings) string {
	if s.Token != "" {
		msg = strings.ReplaceAll(msg, s.Token, "[redacted]")
	}
	return msg
}

func (s Settings) sanitized() map[string]any {
	return map[string]any{
		"enabled":                 s.Enabled,
		"db_path":                 s.dbPath(),
		"default_range_days":      s.DefaultRangeDays,
		"default_limit":           s.DefaultLimit,
		"request_timeout_seconds": s.RequestTimeoutSeconds,
		"dashboard_token":         s.Token != "",
	}
}

func parseBoolValue(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func configPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".pegg", "pegg.json")
	}
	return filepath.Join("~", ".pegg", "pegg.json")
}

func persistSettings(settings Settings) (string, error) {
	path := configPath()
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
	} else if len(data) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return "", fmt.Errorf("parse %s: %w", path, err)
		}
	}
	plugins, ok := root["plugins"].(map[string]any)
	if !ok {
		plugins = map[string]any{}
		root["plugins"] = plugins
	}
	options, ok := plugins["options"].(map[string]any)
	if !ok {
		options = map[string]any{}
		plugins["options"] = options
	}
	key := pluginName
	if _, ok := options[pluginName]; !ok {
		if _, ok := options["pegg-"+pluginName]; ok {
			key = "pegg-" + pluginName
		}
	}
	entry, ok := options[key].(map[string]any)
	if !ok {
		entry = map[string]any{}
	}
	entry["enabled"] = settings.Enabled
	entry["db_path"] = settings.DBPath
	entry["default_range_days"] = settings.DefaultRangeDays
	entry["default_limit"] = settings.DefaultLimit
	entry["request_timeout_seconds"] = settings.RequestTimeoutSeconds
	options[key] = entry

	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	encoded = append(encoded, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pegg-plugin-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("set config permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("replace %s: %w", path, err)
	}
	return path, nil
}

func parseInt(value string) (int, error) {
	var n int
	_, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &n)
	return n, err
}
