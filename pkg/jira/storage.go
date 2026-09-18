package jira

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	configMu         sync.RWMutex
	customConfigPath string
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
	if flag.Lookup("test.v") != nil {
		return true
	}
	execName := filepath.Base(os.Args[0])
	if strings.HasSuffix(execName, ".test") || strings.Contains(execName, "test") || strings.Contains(os.Args[0], "/_test/") {
		return true
	}
	return false
}

func getConfigFilePath() string {
	configMu.RLock()
	custom := customConfigPath
	configMu.RUnlock()

	if custom != "" {
		dir := filepath.Dir(custom)
		_ = os.MkdirAll(dir, 0755)
		return custom
	}

	if envPath := os.Getenv("JIRA_QUICK_ACCESS_CONFIG"); envPath != "" {
		dir := filepath.Dir(envPath)
		_ = os.MkdirAll(dir, 0755)
		return envPath
	}

	// Critical safety guard: NEVER write to user's real home directory during any test run!
	if isRunningInTest() {
		testDir := filepath.Join(os.TempDir(), "jira-quick-access-test")
		_ = os.MkdirAll(testDir, 0755)
		return filepath.Join(testDir, "config.json")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".jira-quick-access")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "config.json")
}

// LoadFromDotEnv reads credentials from a local .env file if available.
func LoadFromDotEnv(cfg *Config) bool {
	data, err := os.ReadFile(".env")
	if err != nil {
		return false
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	loaded := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "JIRA_BASE_URL=") {
			cfg.BaseURL = strings.TrimSpace(strings.TrimPrefix(line, "JIRA_BASE_URL="))
			loaded = true
		} else if strings.HasPrefix(line, "JIRA_EMAIL=") {
			cfg.Email = strings.TrimSpace(strings.TrimPrefix(line, "JIRA_EMAIL="))
			loaded = true
		} else if strings.HasPrefix(line, "JIRA_API_TOKEN=") {
			cfg.APIToken = strings.TrimSpace(strings.TrimPrefix(line, "JIRA_API_TOKEN="))
			loaded = true
		} else if strings.HasPrefix(line, "ATATT") {
			cfg.APIToken = line
			loaded = true
		}
	}

	cfg.EnsureInstances()
	if loaded && len(cfg.Instances) > 0 {
		if cfg.BaseURL != "" {
			cfg.Instances[0].BaseURL = cfg.BaseURL
		}
		if cfg.Email != "" {
			cfg.Instances[0].Email = cfg.Email
		}
		if cfg.APIToken != "" {
			cfg.Instances[0].APIToken = cfg.APIToken
		}
	}
	return loaded
}

// LoadConfig loads configuration from disk, returning defaults if not found.
func LoadConfig() Config {
	cfg := DefaultConfig()
	filePath := getConfigFilePath()

	data, err := os.ReadFile(filePath)
	if err != nil {
		// Attempt fallback to .bak if primary config is missing
		data, err = os.ReadFile(filePath + ".bak")
	}
	if err == nil {
		var saved Config
		if err := json.Unmarshal(data, &saved); err == nil {
			if len(saved.Instances) > 0 {
				cfg.Instances = saved.Instances
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
		}
	} else {
		// First launch: No config.json exists on disk yet.
		// Check if .env supplies credentials; otherwise enable Demo Mode for out-of-the-box preview.
		hasDotEnv := LoadFromDotEnv(&cfg)
		if !hasDotEnv && (len(cfg.Instances) == 0 || cfg.Instances[0].APIToken == "") {
			cfg.DemoMode = true
		}
	}

	cfg.EnsureInstances()

	// If APIToken is empty, try loading from .env
	if cfg.Instances[0].APIToken == "" || cfg.Instances[0].Email == "" {
		LoadFromDotEnv(&cfg)
	}

	return cfg
}

// SaveConfig writes the configuration to disk atomically and creates safety backups.
func SaveConfig(cfg Config) error {
	cfg.EnsureInstances()
	filePath := getConfigFilePath()
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// 1. Safety Backup: If filePath already exists with non-empty content, back it up
	if existing, err := os.ReadFile(filePath); err == nil && len(existing) > 0 {
		_ = os.WriteFile(filePath+".bak", existing, 0600)

		// Create timestamped history backup (retaining last 10 revisions)
		if !isRunningInTest() {
			backupDir := filepath.Join(dir, "backups")
			if err := os.MkdirAll(backupDir, 0755); err == nil {
				timestamp := time.Now().Format("20060102-150405")
				backupFile := filepath.Join(backupDir, fmt.Sprintf("config-%s.json", timestamp))
				_ = os.WriteFile(backupFile, existing, 0600)
				pruneOldBackups(backupDir, 10)
			}
		}
	}

	// 2. Atomic write: write to temp file first, then atomically rename
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
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
