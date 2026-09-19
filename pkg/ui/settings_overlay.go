package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
	"jira-quick-access/pkg/window"
)

func settingsButton(canvas widget.Canvas, r geometry.Rect, text string, selected bool) {
	bg, border := widget.RGBA8(32, 42, 60, 255), widget.RGBA8(255, 255, 255, 35)
	if selected {
		bg, border = widget.RGBA8(36, 86, 150, 255), widget.RGBA8(96, 165, 250, 180)
	}
	canvas.DrawRoundRect(r, bg, 6)
	canvas.StrokeRoundRect(r, border, 6, 1)
	canvas.PushClip(r)
	canvas.DrawText(text, r, 11, widget.RGBA8(228, 237, 250, 255), selected, widget.TextAlignCenter)
	canvas.PopClip()
}

func settingsText(canvas widget.Canvas, r geometry.Rect, text string, size float32, heading bool) {
	color := widget.RGBA8(155, 174, 198, 255)
	if heading {
		color = widget.RGBA8(238, 245, 255, 255)
	}
	canvas.DrawText(text, r, size, color, heading, widget.TextAlignLeft)
}

func settingsCard(canvas widget.Canvas, r geometry.Rect) {
	canvas.DrawRoundRect(r, widget.RGBA8(23, 30, 44, 255), 10)
	canvas.StrokeRoundRect(r, widget.RGBA8(121, 151, 191, 45), 10, 1)
}

func (v *AppView) drawSettingsOverlay(ctx widget.Context, canvas widget.Canvas, r geometry.Rect, s appViewStateSnapshot) {
	l := newSettingsLayout(r, len(s.instances), s.selectedInstIdx, len(s.monitors))
	rect := func(x, y, w, h float32) geometry.Rect { return geometry.NewRect(r.Min.X+x, r.Min.Y+y, w, h) }
	canvas.DrawRoundRect(r, widget.RGBA8(16, 21, 31, 252), 14)
	canvas.StrokeRoundRect(r, widget.RGBA8(255, 255, 255, 65), 14, 1)
	settingsText(canvas, rect(20, 14, 90, 24), "Settings", 17, true)
	if s.version != "" {
		badge := rect(106, 18, 66, 19)
		canvas.DrawRoundRect(badge, widget.RGBA8(35, 47, 66, 255), 4)
		canvas.DrawText("v"+s.version, badge, 10, widget.RGBA8(150, 188, 232, 255), false, widget.TextAlignCenter)
	}
	settingsButton(canvas, l.close, "Close", false)
	settingsButton(canvas, l.instancesTab, "Jira instances", s.settingsSection == settingsInstances)
	settingsButton(canvas, l.applicationTab, "Application", s.settingsSection == settingsApplication)
	settingsText(canvas, rect(350, 56, r.Width()-380, 16), "Connections and app preferences", 10, false)

	if s.settingsSection == settingsApplication {
		v.drawApplicationSettings(canvas, l, s)
	} else {
		v.drawInstanceSettings(canvas, l, s)
	}

	footerY := r.Height() - 48
	canvas.DrawLine(geometry.Pt(r.Min.X+20, r.Min.Y+footerY), geometry.Pt(r.Max.X-20, r.Min.Y+footerY), widget.RGBA8(255, 255, 255, 35), 1)
	settingsText(canvas, rect(20, footerY+13, r.Width()-192, 16), "Save applies to all Jira instances and application settings.", 10, false)
	canvas.DrawRoundRect(l.save, widget.RGBA8(96, 165, 250, 255), 6)
	canvas.DrawText("Save all settings", l.save, 11, widget.RGBA8(13, 24, 42, 255), true, widget.TextAlignCenter)
	if s.statusMsg != "" {
		message := rect(32, 494, r.Width()-64, 16)
		canvas.PushClip(message)
		canvas.DrawText(s.statusMsg, message, 10, ColorStatusInProgress, false, widget.TextAlignLeft)
		canvas.PopClip()
	}
}

func (v *AppView) drawInstanceSettings(canvas widget.Canvas, l settingsLayout, s appViewStateSnapshot) {
	r := l.bounds
	rect := func(x, y, w, h float32) geometry.Rect { return geometry.NewRect(r.Min.X+x, r.Min.Y+y, w, h) }
	settingsCard(canvas, rect(20, 92, r.Width()-40, 422))
	settingsText(canvas, rect(32, 102, 250, 20), "Selected Jira instance", 13, true)
	settingsText(canvas, rect(32, 124, r.Width()-64, 14), "Credentials, ticket query and accent color belong to this instance only.", 10, false)
	for _, tab := range l.instanceTabs {
		name := s.instances[tab.index].Name
		if name == "" {
			name = fmt.Sprintf("Instance %d", tab.index+1)
		}
		maxChars := max(4, int((tab.rect.Width()-12)/6))
		settingsButton(canvas, tab.rect, truncateSummary(name, maxChars), tab.index == s.selectedInstIdx)
	}
	if l.previousInstances.Width() > 0 {
		settingsButton(canvas, l.previousInstances, "‹", false)
	}
	if l.nextInstances.Width() > 0 {
		settingsButton(canvas, l.nextInstances, "›", false)
	}
	settingsButton(canvas, l.addInstance, "+ Add", false)
	labels := []string{"Instance name", "Jira base URL", "User email", "API token", "Ticket query (JQL)"}
	values := []string{s.nameVal, s.urlVal, s.emailVal, s.tokenVal, s.jqlVal}
	for i, label := range labels {
		field := l.fields[i]
		settingsText(canvas, geometry.NewRect(field.Min.X, field.Min.Y-16, field.Width(), 14), label, 10, false)
		drawSettingsInput(canvas, field, i+1, values[i], s)
	}
	settingsText(canvas, rect(32, 405, 200, 14), "Instance accent color", 10, false)
	current := s.colorVal
	if current == "" {
		current = "#38bdf8"
	}
	for i, preset := range ProfilePresets {
		center := l.colorCenters[i]
		if strings.EqualFold(current, preset.Hex) {
			canvas.DrawCircle(center, 11, widget.RGBA8(240, 247, 255, 255))
		}
		canvas.DrawCircle(center, 8, preset.Color)
	}
	settingsText(canvas, rect(278, 429, 100, 16), current, 10, false)
	settingsButton(canvas, l.importEnv, "Import .env", false)
	settingsButton(canvas, l.testConnection, "Test connection", false)
	if len(s.instances) > 1 {
		canvas.DrawRoundRect(l.deleteInstance, widget.RGBA8(62, 30, 41, 255), 6)
		canvas.StrokeRoundRect(l.deleteInstance, widget.RGBA8(220, 90, 112, 100), 6, 1)
		canvas.DrawText("Remove instance", l.deleteInstance, 11, widget.RGBA8(255, 172, 189, 255), false, widget.TextAlignCenter)
	}
}

func drawSettingsInput(canvas widget.Canvas, r geometry.Rect, field int, raw string, s appViewStateSnapshot) {
	focused := s.activeField == field
	border := widget.RGBA8(255, 255, 255, 30)
	if focused {
		border = ColorStatusToDo
	}
	canvas.DrawRoundRect(r, widget.RGBA8(13, 18, 28, 255), 5)
	canvas.StrokeRoundRect(r, border, 5, 1)
	display := raw
	if field == 4 && !focused {
		display = maskToken(raw)
	}
	textRect := geometry.NewRect(r.Min.X+8, r.Min.Y+6, r.Width()-16, 14)
	canvas.PushClip(geometry.NewRect(r.Min.X+4, r.Min.Y+2, r.Width()-8, r.Height()-4))
	if display == "" && !focused {
		settingsText(canvas, textRect, "Click to enter or paste", 11, false)
	} else {
		if focused && s.selectAll && display != "" {
			width := min(measureTextWidth(display, 11)+4, r.Width()-12)
			canvas.DrawRoundRect(geometry.NewRect(textRect.Min.X-2, textRect.Min.Y-1, width, 16), widget.RGBA8(59, 130, 246, 180), 2)
		}
		canvas.DrawText(display, textRect, 11, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignLeft)
		if focused && !s.selectAll && (time.Now().UnixMilli()/500)%2 == 0 {
			cp := max(0, min(s.cursorPos, len(raw)))
			x := min(textRect.Min.X+measureTextWidth(raw[:cp], 11)+1, r.Max.X-6)
			canvas.DrawLine(geometry.Pt(x, r.Min.Y+4), geometry.Pt(x, r.Max.Y-4), widget.RGBA8(255, 255, 255, 240), 1.5)
		}
	}
	canvas.PopClip()
}

func (v *AppView) drawApplicationSettings(canvas widget.Canvas, l settingsLayout, s appViewStateSnapshot) {
	r := l.bounds
	rect := func(x, y, w, h float32) geometry.Rect { return geometry.NewRect(r.Min.X+x, r.Min.Y+y, w, h) }
	settingsText(canvas, rect(20, 96, 250, 20), "Application preferences", 13, true)
	settingsText(canvas, rect(20, 118, r.Width()-40, 14), "These settings apply to every Jira instance on this device.", 11, false)

	settingsCard(canvas, rect(20, 144, r.Width()-40, 110))
	settingsText(canvas, rect(32, 154, 210, 16), "Window & display", 12, true)
	settingsText(canvas, rect(r.Width()-222, 156, 190, 14), "Applies and saves immediately", 10, false)
	settingsButton(canvas, l.leftEdge, "Left edge", s.dockSide == window.DockSideLeft)
	settingsButton(canvas, l.rightEdge, "Right edge", s.dockSide == window.DockSideRight)
	aot := "Always on top: Off"
	if s.alwaysOnTop {
		aot = "Always on top: On"
	}
	settingsButton(canvas, l.alwaysOnTop, aot, s.alwaysOnTop)
	ah := "Auto-hide: Off"
	if s.autoHide {
		ah = "Auto-hide: On"
	}
	settingsButton(canvas, l.autoHide, ah, s.autoHide)
	settingsText(canvas, rect(32, 222, 96, 14), "Display", 11, false)
	if len(s.monitors) == 0 {
		settingsText(canvas, rect(136, 222, 230, 14), "No displays detected", 11, false)
	}
	for i, mon := range s.monitors {
		label := fmt.Sprintf("Display %d", mon.Index+1)
		if l.monitorButtons[i].Width() < 70 {
			label = fmt.Sprintf("%d", mon.Index+1)
		}
		settingsButton(canvas, l.monitorButtons[i], label, s.selectedMonitor == mon.Index)
	}

	settingsCard(canvas, rect(20, 266, r.Width()-40, 150))
	settingsText(canvas, rect(32, 277, 220, 16), "Background updates", 12, true)
	settingsText(canvas, rect(310, 279, r.Width()-342, 14), "Intervals are in seconds", 10, false)
	settingsText(canvas, rect(32, 316, 170, 14), "Jira ticket refresh", 11, false)
	drawSettingsInput(canvas, l.fields[5], 6, s.intervalVal, s)
	settingsText(canvas, rect(310, 316, r.Width()-342, 14), "Refreshes tickets across all instances", 10, false)
	settingsText(canvas, rect(32, 352, 170, 14), "Atlassian health refresh", 11, false)
	drawSettingsInput(canvas, l.fields[6], 7, s.statusIntervalVal, s)
	statusText := "Health monitoring: Off"
	if s.config.StatusCheckEnabled {
		statusText = "Health monitoring: On"
	}
	settingsButton(canvas, l.statusEnabled, statusText, s.config.StatusCheckEnabled)
	settingsText(canvas, rect(32, 389, 170, 14), "Set both intervals", 11, false)
	for i, label := range []string{"1 min", "5 min", "15 min"} {
		settingsButton(canvas, l.intervalPresets[i], label, false)
	}

	settingsCard(canvas, rect(20, 428, r.Width()-40, 60))
	settingsText(canvas, rect(32, 436, 250, 14), "Data source & diagnostics", 11, true)
	mode := "Data: Live Jira"
	if s.demoMode {
		mode = "Data: Demo tickets"
	}
	settingsButton(canvas, l.demoMode, mode, s.demoMode)
	debug := "Debug logging: Off"
	if s.debugMode {
		debug = "Debug logging: On"
	}
	settingsButton(canvas, l.debugMode, debug, s.debugMode)
	settingsText(canvas, rect(346, 462, r.Width()-378, 14), "Use Save all settings to apply", 10, false)
}
