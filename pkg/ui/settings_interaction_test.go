package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

func settingsTestCenter(r geometry.Rect) geometry.Point {
	return geometry.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

func settingsTestLayout(v *AppView) settingsLayout {
	v.mu.Lock()
	defer v.mu.Unlock()
	x := float32(8)
	if v.dockSide == window.DockSideLeft {
		x = 118
	}
	return newSettingsLayout(geometry.NewRect(x, 8, 654, 564), len(v.config.Instances), v.selectedInstIdx, len(v.monitors))
}

func TestSettingsPageSwitchPreservesEditsAndFocus(t *testing.T) {
	v, _ := createTestAppView()
	defer v.Close()
	v.OpenSettings()
	v.mu.Lock()
	v.nameVal = "Edited instance"
	v.activeField = 1
	v.selectAll = true
	v.mu.Unlock()
	v.handleClick(settingsTestCenter(settingsTestLayout(v).applicationTab))
	if v.settingsSection != settingsApplication || v.activeField != 0 || v.selectAll {
		t.Fatal("section switch did not reset focus")
	}
	v.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyTab, 0, event.ModNone))
	if v.activeField != 6 {
		t.Fatalf("expected first application field, got %d", v.activeField)
	}
	v.intervalVal = "123"
	for _, want := range []int{7, 6, 7, 6} {
		v.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyTab, 0, event.ModNone))
		if v.activeField != want {
			t.Fatalf("expected field %d, got %d", want, v.activeField)
		}
	}
	v.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyTab, 0, event.ModShift))
	if v.activeField != 7 {
		t.Fatal("reverse tab did not wrap within application fields")
	}
	v.handleClick(settingsTestCenter(settingsTestLayout(v).instancesTab))
	if v.nameVal != "Edited instance" || v.intervalVal != "123" {
		t.Fatal("switching sections lost draft values")
	}
	v.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyTab, 0, event.ModShift))
	if v.activeField != 5 {
		t.Fatal("reverse tab did not start at last instance field")
	}
	v.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if v.activeField != 1 {
		t.Fatal("instance keyboard focus escaped its page")
	}
	v.activeField = 6
	v.handleKey(event.NewKeyEvent(event.KeyPress, event.KeySpace, '1', event.ModNone))
	if v.intervalVal != "123" {
		t.Fatal("hidden field accepted keyboard input")
	}
}

func TestSettingsInstanceOverflowAndScopedActions(t *testing.T) {
	v, _ := createTestAppView()
	defer v.Close()
	v.OpenSettings()
	for i := 0; i < 7; i++ {
		v.handleClick(settingsTestCenter(settingsTestLayout(v).addInstance))
	}
	if len(v.config.Instances) != 9 || v.selectedInstIdx != 8 {
		t.Fatal("adding instances beyond first page failed")
	}
	l := settingsTestLayout(v)
	if l.previousInstances.Width() <= 0 || l.instanceTabs[0].index != 8 {
		t.Fatal("newly added instance not on visible page")
	}
	v.handleClick(settingsTestCenter(l.previousInstances))
	if v.selectedInstIdx != 7 {
		t.Fatal("previous page was not reachable")
	}
	v.handleClick(settingsTestCenter(settingsTestLayout(v).nextInstances))
	if v.selectedInstIdx != 8 {
		t.Fatal("next page was not reachable")
	}
	v.handleClick(settingsTestCenter(settingsTestLayout(v).applicationTab))
	count := len(v.config.Instances)
	v.handleClick(settingsTestCenter(settingsTestLayout(v).deleteInstance))
	if len(v.config.Instances) != count {
		t.Fatal("hidden Remove instance action was clickable")
	}
	v.handleClick(settingsTestCenter(settingsTestLayout(v).intervalPresets[0]))
	if v.intervalVal != "60" || v.statusIntervalVal != "60" {
		t.Fatal("preset did not update both intervals")
	}
	v.handleClick(settingsTestCenter(settingsTestLayout(v).instancesTab))
	v.handleClick(settingsTestCenter(settingsTestLayout(v).deleteInstance))
	if len(v.config.Instances) != count-1 || v.selectedInstIdx != count-2 {
		t.Fatal("removal did not select remaining instance")
	}
}

func TestSettingsEnvironmentImportTargetsSelectedInstance(t *testing.T) {
	v, _ := createTestAppView()
	defer v.Close()
	v.OpenSettings()
	v.selectSettingsInstance(1)
	before := v.config.Clone()
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("JIRA_BASE_URL=https://imported.example\nJIRA_EMAIL=imported@example.com\nJIRA_API_TOKEN=test-import-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v.handleClick(settingsTestCenter(settingsTestLayout(v).importEnv))
	if v.config.Instances[0] != before.Instances[0] {
		t.Fatal("import changed the first, unselected instance")
	}
	if v.urlVal != "https://imported.example" || v.emailVal != "imported@example.com" || v.tokenVal != "test-import-token" {
		t.Fatal("selected instance did not receive imported credentials")
	}
	if v.config.Instances[1].ID != before.Instances[1].ID || v.nameVal != before.Instances[1].Name {
		t.Fatal("import overwrote instance identity")
	}
}

func TestSettingsSaveFailureKeepsEditorOpen(t *testing.T) {
	v, client := createTestAppView()
	defer v.Close()
	before := client.GetConfig()
	path := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(path, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	jira.SetConfigFilePathForTesting(filepath.Join(path, "config.json"))
	t.Cleanup(jira.ResetConfigFilePathForTesting)
	v.OpenSettings()
	v.nameVal = "Unsaved"
	v.saveSettings()
	if !v.showSettings || v.statusMsg == "" || v.nameVal != "Unsaved" {
		t.Fatal("failed save discarded the editor or draft")
	}
	if got := client.GetConfig(); got.Instances[0] != before.Instances[0] {
		t.Fatal("failed save changed running client configuration")
	}
}

func TestSettingsSaveRespectsExplicitDemoMode(t *testing.T) {
	v, client := createTestAppView()
	defer v.Close()
	path := filepath.Join(t.TempDir(), "config.json")
	jira.SetConfigFilePathForTesting(path)
	t.Cleanup(jira.ResetConfigFilePathForTesting)
	v.OpenSettings()
	v.mu.Lock()
	v.demoMode = true
	v.config.StatusCheckEnabled = false
	v.mu.Unlock()
	v.saveSettings()
	if !client.GetConfig().DemoMode {
		t.Fatal("save overrode explicit Demo mode because credentials exist")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(fmt.Errorf("config was not saved: %w", err))
	}
}
