package jira

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func reviewConfigPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	SetConfigFilePathForTesting(path)
	t.Cleanup(ResetConfigFilePathForTesting)
	return path
}

func writeReviewConfig(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentConfigSaves(t *testing.T) {
	path := reviewConfigPath(t)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("instance-%d", i)
			cfg := Config{Instances: []InstanceConfig{{Name: name}}, BranchPrefix: name}
			if err := SaveConfig(cfg); err != nil {
				t.Error(err)
			}
			if cfg.Instances[0].ID != "" {
				t.Error("SaveConfig mutated caller-owned instances")
			}
		}(i)
	}
	wg.Wait()
	for _, file := range []string{path, path + ".bak"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		if len(cfg.Instances) != 1 || cfg.Instances[0].Name != cfg.BranchPrefix {
			t.Fatalf("incomplete saved snapshot: %+v", cfg)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe permissions on %s: %v", file, info.Mode())
		}
	}
	matches, err := filepath.Glob(path + ".tmp.*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain: %v, %v", matches, err)
	}
}

func TestConfigRecoveryAndBackupPreservation(t *testing.T) {
	path := reviewConfigPath(t)
	original := Config{Instances: []InstanceConfig{{ID: "one", Name: "original", Email: "user", APIToken: "token"}}}
	if err := SaveConfig(original); err != nil {
		t.Fatal(err)
	}
	updated := original.Clone()
	updated.Instances[0].Name = "updated"
	if err := SaveConfig(updated); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(); got.Instances[0].Name != "updated" {
		t.Fatal("valid primary was not preferred")
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	writeReviewConfig(t, path, []byte(`{"instances":`))
	if got := LoadConfig(); got.Instances[0].Name != "original" {
		t.Fatal("invalid primary did not recover from backup")
	}
	if err := SaveConfig(updated); err != nil {
		t.Fatal(err)
	}
	preserved, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved) != string(backup) {
		t.Fatal("corrupt primary replaced good backup")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(); got.Instances[0].Name != "original" {
		t.Fatal("missing primary did not recover from backup")
	}
}

func TestInvalidConfigAndBackupUseFirstLaunchDefaults(t *testing.T) {
	path := reviewConfigPath(t)
	writeReviewConfig(t, path, []byte(`{"broken":`))
	writeReviewConfig(t, path+".bak", []byte(`{"also broken":`))
	cfg := LoadConfig()
	if !cfg.DemoMode || cfg.PollInterval != DefaultConfig().PollInterval {
		t.Fatal("invalid config and backup did not use first-launch defaults")
	}
}

func TestConfigTemporaryFileCleanupOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "destination")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigFile(path, []byte(`{}`)); err == nil {
		t.Fatal("expected rename over directory to fail")
	}
	matches, err := filepath.Glob(path + ".tmp.*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain after failure: %v, %v", matches, err)
	}
}
