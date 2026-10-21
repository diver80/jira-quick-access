package jira

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	configMu         sync.RWMutex
	customConfigPath string
	saveMu           sync.Mutex // Serializes SaveConfig to prevent concurrent writes/backups
)

// SetConfigFilePathForTesting overrides the configuration file path (e.g. for testing).
func SetConfigFilePathForTesting(path string) {
	configMu.Lock()
	defer configMu.Unlock()
	customConfigPath = path
}

// ResetConfigFilePathForTesting resets any custom configuration file path override.
func ResetConfigFilePathForTesting() {
	configMu.Lock()
	defer configMu.Unlock()
	customConfigPath = ""
}

func isRunningInTest() bool {
	return testing.Testing()
}

func getConfigFilePath() string {
	configMu.RLock()
	custom := customConfigPath
	configMu.RUnlock()

	if custom != "" {
		dir := filepath.Dir(custom)
		_ = os.MkdirAll(dir, 0700)
		return custom
	}

	if envPath := os.Getenv("JIRA_QUICK_ACCESS_CONFIG"); envPath != "" {
		dir := filepath.Dir(envPath)
		_ = os.MkdirAll(dir, 0700)
		return envPath
	}

	// Critical safety guard: NEVER write to user's real home directory during any test run!
	if isRunningInTest() {
		testDir := filepath.Join(os.TempDir(), "jira-quick-access-test")
		_ = os.MkdirAll(testDir, 0700)
		return filepath.Join(testDir, "config.json")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".jira-quick-access")
	_ = os.MkdirAll(dir, 0700)
	_ = os.Chmod(dir, 0700) // tighten directories created by older versions with 0755
	return filepath.Join(dir, "config.json")
}

// LoadFromDotEnv reads credentials from a local .env file if available.
// A base URL is only taken together with a token from the same file, so a stray
// .env can never redirect an already stored token to another host.
func LoadFromDotEnv(cfg *Config) bool {
	data, err := os.ReadFile(".env")
	if err != nil {
		return false
	}

	var baseURL, email, token string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "JIRA_BASE_URL=") {
			baseURL = strings.TrimSpace(strings.TrimPrefix(line, "JIRA_BASE_URL="))
		} else if strings.HasPrefix(line, "JIRA_EMAIL=") {
			email = strings.TrimSpace(strings.TrimPrefix(line, "JIRA_EMAIL="))
		} else if strings.HasPrefix(line, "JIRA_API_TOKEN=") {
			token = strings.TrimSpace(strings.TrimPrefix(line, "JIRA_API_TOKEN="))
		} else if strings.HasPrefix(line, "ATATT") && !strings.ContainsAny(line, "= ") {
			token = line
		}
	}
	if token == "" {
		return false
	}

	cfg.BaseURL, cfg.APIToken = firstNonEmpty(baseURL, cfg.BaseURL), token
	if email != "" {
		cfg.Email = email
	}
	cfg.EnsureInstances()
	inst := &cfg.Instances[0]
	if baseURL != "" {
		inst.BaseURL = baseURL
	}
	if email != "" {
		inst.Email = email
	}
	inst.APIToken = token
	return true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// LoadConfig loads configuration from disk, returning defaults if not found.
func LoadConfig() Config {
	cfg := DefaultConfig()
	filePath := getConfigFilePath()

	data, err := os.ReadFile(filePath)
	var saved Config
	if err == nil {
		err = json.Unmarshal(data, &saved)
	}
	if err != nil {
		// Missing or corrupt primary: recover the last valid backup.
		data, err = os.ReadFile(filePath + ".bak")
		saved = Config{}
		if err == nil {
			err = json.Unmarshal(data, &saved)
		}
	}

	if err == nil {
		if len(saved.Instances) > 0 {
			cfg.Instances = saved.Instances
		} else if saved.BaseURL != "" || saved.APIToken != "" {
			// Legacy single-instance config: let EnsureInstances migrate the top-level fields.
			cfg.Instances = nil
		}
		if saved.ActiveInstID != "" {
			cfg.ActiveInstID = saved.ActiveInstID
		}
		if saved.BaseURL != "" {
			cfg.BaseURL = saved.BaseURL
		}
		if saved.Email != "" {
			cfg.Email = saved.Email
		}
		if saved.APIToken != "" {
			cfg.APIToken = saved.APIToken
		}
		if saved.JQLQuery != "" {
			cfg.JQLQuery = saved.JQLQuery
		}
		if saved.PollInterval > 0 {
			cfg.PollInterval = saved.PollInterval
		}
		if bytes.Contains(data, []byte("\"status_check_enabled\"")) {
			cfg.StatusCheckEnabled = saved.StatusCheckEnabled
		} else {
			cfg.StatusCheckEnabled = true
		}
		if saved.StatusPollInterval > 0 {
			cfg.StatusPollInterval = saved.StatusPollInterval
		} else {
			cfg.StatusPollInterval = 300
		}
		if saved.BranchPrefix != "" {
			cfg.BranchPrefix = saved.BranchPrefix
		}
		if len(saved.PinnedKeys) > 0 {
			cfg.PinnedKeys = saved.PinnedKeys
		}
		cfg.DemoMode = saved.DemoMode
		cfg.DebugMode = saved.DebugMode
		cfg.DockSide = saved.DockSide
		cfg.MonitorIndex = saved.MonitorIndex
		if saved.PosYRatio > 0 && saved.PosYRatio <= 1.0 {
			cfg.PosYRatio = saved.PosYRatio
		} else {
			cfg.PosYRatio = 0.5
		}
		if bytes.Contains(data, []byte("\"always_on_top\"")) {
			cfg.AlwaysOnTop = saved.AlwaysOnTop
		} else {
			cfg.AlwaysOnTop = true
		}
		cfg.AutoHide = saved.AutoHide
		// Features added in 1.1 default to on for configs written by older versions.
		cfg.GlobalHotKey = saved.GlobalHotKey || !bytes.Contains(data, []byte("\"global_hotkey\""))
		cfg.Notifications = saved.Notifications || !bytes.Contains(data, []byte("\"notifications\""))
		cfg.UpdateCheck = saved.UpdateCheck || !bytes.Contains(data, []byte("\"update_check\""))
		cfg.SortMode = saved.SortMode
		cfg.ChangesHideOwn = saved.ChangesHideOwn
	} else {
		// First launch: No config.json exists on disk yet.
		// Check if .env supplies credentials; otherwise enable Demo Mode for out-of-the-box preview.
		hasDotEnv := LoadFromDotEnv(&cfg)
		if !hasDotEnv && (len(cfg.Instances) == 0 || cfg.Instances[0].APIToken == "") {
			cfg.DemoMode = true
		}
	}

	cfg.EnsureInstances()

	// Without a stored token, fall back to a developer .env in the working directory.
	if cfg.Instances[0].APIToken == "" {
		LoadFromDotEnv(&cfg)
	}

	return cfg
}

// SaveConfig writes the configuration to disk atomically and creates safety backups.
func SaveConfig(cfg Config) error {
	// Serialize saves to prevent concurrent write/backup collisions
	saveMu.Lock()
	defer saveMu.Unlock()

	cfg = cfg.Clone()
	cfg.EnsureInstances()
	filePath := getConfigFilePath()
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// 1. Safety Backup (serialized): If filePath already exists with non-empty content, back it up
	// Only backup if it's valid JSON (don't overwrite good backup with corrupt original)
	if existing, err := os.ReadFile(filePath); err == nil && len(existing) > 0 {
		var existingCfg Config
		if err := json.Unmarshal(existing, &existingCfg); err == nil {
			// Primary is valid; back it up
			if err := writeConfigFile(filePath+".bak", existing); err != nil {
				return fmt.Errorf("back up configuration: %w", err)
			}

			// Create timestamped history backup (retaining last 10 revisions)
			if !isRunningInTest() {
				backupDir := filepath.Join(dir, "backups")
				if err := os.MkdirAll(backupDir, 0700); err == nil {
					timestamp := time.Now().Format("20060102-150405")
					backupFile := filepath.Join(backupDir, fmt.Sprintf("config-%s.json", timestamp))
					_ = os.WriteFile(backupFile, existing, 0600)
					pruneOldBackups(backupDir, 10)
				}
			}
		}
		// If primary is corrupt, skip backup to preserve any existing valid .bak
	}

	return writeConfigFile(filePath, data)
}

// writeConfigFile atomically replaces a config or backup with a private file.
func writeConfigFile(filePath string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(filePath), filepath.Base(filePath)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filePath)
}

func pruneOldBackups(dir string, maxBackups int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backupFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "config-") && strings.HasSuffix(entry.Name(), ".json") {
			backupFiles = append(backupFiles, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(backupFiles)
	if len(backupFiles) > maxBackups {
		toDelete := backupFiles[:len(backupFiles)-maxBackups]
		for _, f := range toDelete {
			_ = os.Remove(f)
		}
	}
}
