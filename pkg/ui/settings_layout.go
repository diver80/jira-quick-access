package ui

import "github.com/gogpu/ui/geometry"

type settingsSection uint8

const (
	settingsInstances settingsSection = iota
	settingsApplication
)

type settingsInstanceTab struct {
	index int
	rect  geometry.Rect
}

// settingsLayout is shared by painting and hit testing, so visible controls and
// their clickable areas cannot drift apart when the settings layout changes.
type settingsLayout struct {
	bounds                                        geometry.Rect
	instancesTab, applicationTab, close, save     geometry.Rect
	importEnv, testConnection, deleteInstance     geometry.Rect
	addInstance, previousInstances, nextInstances geometry.Rect
	instanceTabs                                  []settingsInstanceTab
	fields                                        [7]geometry.Rect
	colorCenters                                  []geometry.Point
	leftEdge, rightEdge, alwaysOnTop, autoHide    geometry.Rect
	monitorButtons                                []geometry.Rect
	statusEnabled, demoMode, debugMode            geometry.Rect
	intervalPresets                               [3]geometry.Rect
}

func newSettingsLayout(r geometry.Rect, instanceCount, selectedInstance, monitorCount int) settingsLayout {
	rect := func(x, y, w, h float32) geometry.Rect {
		return geometry.NewRect(r.Min.X+x, r.Min.Y+y, w, h)
	}
	w := r.Width()
	l := settingsLayout{
		bounds:       r,
		instancesTab: rect(20, 48, 154, 30), applicationTab: rect(180, 48, 154, 30),
		close: rect(w-76, 14, 56, 26), save: rect(w-156, r.Height()-40, 136, 28),
		importEnv: rect(32, 460, 94, 28), testConnection: rect(134, 460, 134, 28),
		deleteInstance: rect(w-158, 460, 126, 28), addInstance: rect(w-112, 144, 80, 26),
		leftEdge: rect(32, 178, 95, 26), rightEdge: rect(135, 178, 95, 26),
		alwaysOnTop: rect(244, 178, 140, 26), autoHide: rect(392, 178, w-424, 26),
		statusEnabled: rect(310, 346, 180, 26),
		demoMode:      rect(32, 454, 150, 28), debugMode: rect(190, 454, 140, 28),
	}
	for i := 0; i < 5; i++ {
		l.fields[i] = rect(32, 196+float32(i)*44, w-64, 26)
	}
	l.fields[5] = rect(212, 310, 74, 26)
	l.fields[6] = rect(212, 346, 74, 26)
	for i := range l.intervalPresets {
		l.intervalPresets[i] = rect(212+float32(i)*70, 382, 62, 26)
	}
	for i := range ProfilePresets {
		l.colorCenters = append(l.colorCenters, geometry.Pt(r.Min.X+42+float32(i)*28, r.Min.Y+436))
	}

	// Keep Add reachable for any number of instances. Four tabs per page leave
	// readable labels; arrow controls page by selecting the next group.
	const pageSize = 4
	selectedInstance = max(0, min(selectedInstance, instanceCount-1))
	start := (selectedInstance / pageSize) * pageSize
	end := min(start+pageSize, instanceCount)
	tabX, tabArea := float32(32), w-156
	if instanceCount > pageSize {
		if start > 0 {
			l.previousInstances = rect(32, 144, 24, 26)
		}
		if end < instanceCount {
			l.nextInstances = rect(w-148, 144, 24, 26)
		}
		tabX += 32
		tabArea -= 64
	}
	if count := end - start; count > 0 {
		tabWidth := min(float32(110), (tabArea-float32(count-1)*6)/float32(count))
		for i := start; i < end; i++ {
			l.instanceTabs = append(l.instanceTabs, settingsInstanceTab{i, rect(tabX+float32(i-start)*(tabWidth+6), 144, tabWidth, 26)})
		}
	}
	// Share available space rather than allowing monitor buttons to overlap
	// the rail. Labels can be shortened independently by the renderer.
	if monitorCount > 0 {
		gap := min(float32(6), (w-168)/float32(monitorCount*2))
		width := min(float32(80), (w-168-gap*float32(monitorCount-1))/float32(monitorCount))
		for i := 0; i < monitorCount; i++ {
			l.monitorButtons = append(l.monitorButtons, rect(136+float32(i)*(width+gap), 216, width, 26))
		}
	}
	return l
}

func (l settingsLayout) visibleFields(section settingsSection) []int {
	if section == settingsApplication {
		return []int{6, 7}
	}
	return []int{1, 2, 3, 4, 5}
}
