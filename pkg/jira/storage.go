package jira

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func getConfigFilePath() string {
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
			if saved.BranchPrefix != "" {
				cfg.BranchPrefix = saved.BranchPrefix
			}
			if len(saved.PinnedKeys) > 0 {
				cfg.PinnedKeys = saved.PinnedKeys
			}
			cfg.DemoMode = saved.DemoMode
			cfg.DebugMode = saved.DebugMode
		}
	}

	cfg.EnsureInstances()

	// If APIToken is empty, try loading from .env
	if cfg.Instances[0].APIToken == "" || cfg.Instances[0].Email == "" {
		LoadFromDotEnv(&cfg)
	}

	return cfg
}

// SaveConfig writes the configuration to disk.
func SaveConfig(cfg Config) error {
	cfg.EnsureInstances()
	filePath := getConfigFilePath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0600)
}
