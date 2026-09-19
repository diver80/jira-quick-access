package ui

import (
	"fmt"
	"testing"

	"github.com/gogpu/ui/geometry"
)

func settingsRectContains(outer, inner geometry.Rect) bool {
	return inner.Min.X >= outer.Min.X && inner.Min.Y >= outer.Min.Y && inner.Max.X <= outer.Max.X && inner.Max.Y <= outer.Max.Y
}

func settingsRectsOverlap(a, b geometry.Rect) bool {
	return a.Min.X < b.Max.X && a.Max.X > b.Min.X && a.Min.Y < b.Max.Y && a.Max.Y > b.Min.Y
}

func TestSettingsLayoutVisibleControlsDoNotOverlap(t *testing.T) {
	for _, x := range []float32{8, 118} {
		for _, count := range []int{1, 4, 5, 13} {
			for selected := 0; selected < count; selected++ {
				for _, section := range []settingsSection{settingsInstances, settingsApplication} {
					t.Run(fmt.Sprintf("x%.0f/count%d/selected%d/page%d", x, count, selected, section), func(t *testing.T) {
						r := geometry.NewRect(x, 8, 654, 564)
						l := newSettingsLayout(r, count, selected, 5)
						controls := []geometry.Rect{l.instancesTab, l.applicationTab, l.close, l.save}
						for _, field := range l.visibleFields(section) {
							controls = append(controls, l.fields[field-1])
						}
						if section == settingsInstances {
							controls = append(controls, l.addInstance, l.importEnv, l.testConnection)
							if count > 1 {
								controls = append(controls, l.deleteInstance)
							}
							found := false
							for _, tab := range l.instanceTabs {
								controls = append(controls, tab.rect)
								if tab.index == selected {
									found = true
								}
							}
							if !found {
								t.Fatal("selected instance not visible")
							}
							for _, button := range []geometry.Rect{l.previousInstances, l.nextInstances} {
								if button.Width() > 0 {
									controls = append(controls, button)
								}
							}
							for _, center := range l.colorCenters {
								controls = append(controls, geometry.NewRect(center.X-12, center.Y-12, 24, 24))
							}
						} else {
							controls = append(controls, l.leftEdge, l.rightEdge, l.alwaysOnTop, l.autoHide, l.statusEnabled, l.demoMode, l.debugMode)
							controls = append(controls, l.monitorButtons...)
							controls = append(controls, l.intervalPresets[:]...)
						}
						for i, control := range controls {
							if control.Width() <= 0 || control.Height() <= 0 || !settingsRectContains(r, control) {
								t.Fatalf("control %d out of bounds: %+v", i, control)
							}
							for j := 0; j < i; j++ {
								if settingsRectsOverlap(control, controls[j]) {
									t.Fatalf("controls %d and %d overlap", i, j)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestSettingsRenderingScope(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()
	view.OpenSettings()
	for _, section := range []settingsSection{settingsInstances, settingsApplication} {
		view.switchSettingsSection(section)
		canvas := &testMockCanvas{}
		r := geometry.NewRect(8, 8, 654, 564)
		view.drawSettingsOverlay(&testMockContext{}, canvas, r, view.snapshot())
		labels := map[string]bool{}
		for _, call := range canvas.getDrawTexts() {
			labels[call.text] = true
			if !settingsRectContains(r, call.bounds) {
				t.Errorf("text %q outside settings", call.text)
			}
		}
		for _, label := range []string{"Settings", "Jira instances", "Application", "Save all settings"} {
			if !labels[label] {
				t.Errorf("missing shared label %q", label)
			}
		}
		if labels["Test connection"] != (section == settingsInstances) || labels["Remove instance"] != (section == settingsInstances) {
			t.Fatal("instance actions leaked across page scopes")
		}
		if labels["Background updates"] != (section == settingsApplication) || labels["Window & display"] != (section == settingsApplication) {
			t.Fatal("application controls leaked across page scopes")
		}
	}
}
