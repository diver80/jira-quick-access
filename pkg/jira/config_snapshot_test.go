package jira

import (
	"sync"
	"testing"
)

func TestClientConfigSnapshots(t *testing.T) {
	cfg := Config{Instances: []InstanceConfig{{Name: "original"}}, PinnedKeys: []string{"TEST-1"}}
	client := NewClient(cfg)
	if cfg.Instances[0].ID != "" {
		t.Fatal("NewClient normalized the caller's configuration")
	}
	cfg.Instances[0].Name = "caller edit"
	cfg.PinnedKeys[0] = "caller pin"
	got := client.GetConfig()
	if got.Instances[0].Name != "original" || got.PinnedKeys[0] != "TEST-1" {
		t.Fatal("client retained caller-owned slices")
	}
	got.Instances[0].Name = "snapshot edit"
	got.PinnedKeys[0] = "snapshot pin"
	if next := client.GetConfig(); next.Instances[0].Name != "original" || next.PinnedKeys[0] != "TEST-1" {
		t.Fatal("GetConfig exposed mutable client slices")
	}
	client.UpdateConfig(cfg)
	cfg.Instances[0].Name = "second edit"
	cfg.PinnedKeys[0] = "second pin"
	if next := client.GetConfig(); next.Instances[0].Name != "caller edit" || next.PinnedKeys[0] != "caller pin" {
		t.Fatal("UpdateConfig retained caller-owned slices")
	}
}

func TestClientConfigConcurrentSnapshotEdits(t *testing.T) {
	client := NewClient(Config{Instances: []InstanceConfig{{ID: "one", Name: "original"}}})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cfg := client.GetConfig()
				cfg.Instances[0].Name = "edited"
				client.UpdateConfig(cfg)
			}
		}()
	}
	wg.Wait()
}
