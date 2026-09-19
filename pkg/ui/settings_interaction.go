package ui

import (
	"fmt"
	"time"

	"github.com/gogpu/ui/geometry"
	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

func (v *AppView) switchSettingsSection(section settingsSection) {
	v.mu.Lock()
	v.saveCurrentInstanceFieldsLocked()
	v.settingsSection = section
	v.activeField, v.cursorPos, v.selectAll = 0, 0, false
	v.statusMsg = ""
	v.mu.Unlock()
	v.MarkNeedsLayout()
}

func (v *AppView) selectSettingsInstance(index int) {
	v.mu.Lock()
	if index >= 0 && index < len(v.config.Instances) {
		v.saveCurrentInstanceFieldsLocked()
		v.loadInstanceFieldsLocked(index)
		v.statusMsg = ""
	}
	v.mu.Unlock()
	v.MarkNeedsLayout()
}

func (v *AppView) handleSettingsClick(r geometry.Rect, pos geometry.Point) bool {
	v.mu.Lock()
	section, selected, count := v.settingsSection, v.selectedInstIdx, len(v.config.Instances)
	monitors := append([]window.MonitorInfo(nil), v.monitors...)
	v.mu.Unlock()
	l := newSettingsLayout(r, count, selected, len(monitors))
	switch {
	case l.instancesTab.Contains(pos):
		v.switchSettingsSection(settingsInstances)
		return true
	case l.applicationTab.Contains(pos):
		v.switchSettingsSection(settingsApplication)
		return true
	case l.close.Contains(pos):
		v.CloseSettings()
		return true
	case l.save.Contains(pos):
		v.saveSettings()
		return true
	}

	if section == settingsInstances {
		for _, tab := range l.instanceTabs {
			if tab.rect.Contains(pos) {
				v.selectSettingsInstance(tab.index)
				return true
			}
		}
		if l.previousInstances.Width() > 0 && l.previousInstances.Contains(pos) {
			v.selectSettingsInstance(l.instanceTabs[0].index - 1)
			return true
		}
		if l.nextInstances.Width() > 0 && l.nextInstances.Contains(pos) {
			v.selectSettingsInstance(l.instanceTabs[len(l.instanceTabs)-1].index + 1)
			return true
		}
		if l.addInstance.Contains(pos) {
			v.mu.Lock()
			v.saveCurrentInstanceFieldsLocked()
			v.config.Instances = append(v.config.Instances, jira.InstanceConfig{
				ID: fmt.Sprintf("inst-%d", time.Now().UnixNano()), Name: fmt.Sprintf("Instance %d", len(v.config.Instances)+1),
				Email: v.emailVal, JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC", Color: "#38bdf8",
			})
			v.loadInstanceFieldsLocked(len(v.config.Instances) - 1)
			v.statusMsg = "New instance added. Use Save all settings to keep it."
			v.invalidateFilterCacheLocked()
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}
		for i, center := range l.colorCenters {
			dx, dy := pos.X-center.X, pos.Y-center.Y
			if dx*dx+dy*dy <= 12*12 {
				v.mu.Lock()
				v.colorVal = ProfilePresets[i].Hex
				v.saveCurrentInstanceFieldsLocked()
				v.mu.Unlock()
				v.MarkNeedsLayout()
				return true
			}
		}
		if l.importEnv.Contains(pos) {
			v.importSettingsEnvironment()
			return true
		}
		if l.testConnection.Contains(pos) {
			v.testConn()
			return true
		}
		if count > 1 && l.deleteInstance.Contains(pos) {
			v.mu.Lock()
			cur := v.selectedInstIdx
			if len(v.config.Instances) > 1 && cur >= 0 && cur < len(v.config.Instances) {
				v.config.Instances = append(v.config.Instances[:cur], v.config.Instances[cur+1:]...)
				v.loadInstanceFieldsLocked(min(cur, len(v.config.Instances)-1))
				if v.activeInstIdx > cur {
					v.activeInstIdx--
				}
				v.activeInstIdx = min(v.activeInstIdx, len(v.config.Instances)-1)
				v.statusMsg = "Instance removed. Use Save all settings to keep this change."
				v.invalidateFilterCacheLocked()
			}
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}
	} else {
		switch {
		case l.leftEdge.Contains(pos):
			v.SetDockSide(window.DockSideLeft)
			return true
		case l.rightEdge.Contains(pos):
			v.SetDockSide(window.DockSideRight)
			return true
		case l.alwaysOnTop.Contains(pos):
			v.mu.Lock()
			enabled := !v.alwaysOnTop
			v.mu.Unlock()
			v.SetAlwaysOnTop(enabled)
			return true
		case l.autoHide.Contains(pos):
			v.mu.Lock()
			enabled := !v.autoHide
			v.mu.Unlock()
			v.SetAutoHide(enabled)
			return true
		}
		for i, button := range l.monitorButtons {
			if button.Contains(pos) {
				v.SetMonitor(monitors[i].Index)
				return true
			}
		}
		v.mu.Lock()
		handled := true
		switch {
		case l.statusEnabled.Contains(pos):
			v.config.StatusCheckEnabled = !v.config.StatusCheckEnabled
		case l.demoMode.Contains(pos):
			v.demoMode = !v.demoMode
		case l.debugMode.Contains(pos):
			v.debugMode = !v.debugMode
		default:
			handled = false
		}
		for i, button := range l.intervalPresets {
			if button.Contains(pos) {
				value := []string{"60", "300", "900"}[i]
				v.intervalVal, v.statusIntervalVal = value, value
				handled = true
			}
		}
		v.mu.Unlock()
		if handled {
			v.MarkNeedsLayout()
			return true
		}
	}

	v.mu.Lock()
	v.activeField, v.selectAll = 0, false
	for _, field := range l.visibleFields(section) {
		input := l.fields[field-1]
		if input.Contains(pos) {
			v.activeField = field
			if target := v.getActiveTargetLocked(); target != nil {
				v.cursorPos = getCursorIndexFromX(*target, 11, pos.X-input.Min.X-8)
			}
			break
		}
	}
	v.mu.Unlock()
	v.MarkNeedsLayout()
	return true
}

// Import into a one-instance snapshot: the .env loader targets Instances[0],
// but the settings action must affect the selected instance, not the first one.
func (v *AppView) importSettingsEnvironment() {
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := v.selectedInstIdx
	if idx < 0 || idx >= len(v.config.Instances) {
		return
	}
	v.saveCurrentInstanceFieldsLocked()
	inst := v.config.Instances[idx]
	cfg := jira.Config{Instances: []jira.InstanceConfig{inst}, BaseURL: inst.BaseURL, Email: inst.Email, APIToken: inst.APIToken}
	if jira.LoadFromDotEnv(&cfg) {
		v.config.Instances[idx] = cfg.Instances[0]
		v.loadInstanceFieldsLocked(idx)
		v.statusMsg = "Imported credentials for this instance. Save to apply."
		v.invalidateFilterCacheLocked()
	} else {
		v.statusMsg = "No Jira credentials found in .env."
	}
	v.MarkNeedsLayout()
}
