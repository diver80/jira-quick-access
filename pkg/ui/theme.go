package ui

import (
	"strings"

	"github.com/gogpu/ui/widget"
)

// CardTheme defines a 3D glassmorphic pastel aesthetic for tickets
type CardTheme struct {
	Name        string
	Background  widget.Color
	Foreground  widget.Color
	Secondary   widget.Color
	Border      widget.Color
	AccentPill  widget.Color
	AccentPillT widget.Color
}

// Crystalline translucent themes inspired by Apple macOS Liquid Glass & Dock styling
var (
	ThemeMint = CardTheme{
		Name:        "Mint",
		Background:  widget.RGBA8(150, 230, 205, 135), // Airy translucent mint
		Foreground:  widget.RGBA8(10, 48, 38, 255),
		Secondary:   widget.RGBA8(25, 78, 62, 220),
		Border:      widget.RGBA8(255, 255, 255, 140),
		AccentPill:  widget.RGBA8(120, 210, 185, 160),
		AccentPillT: widget.RGBA8(10, 45, 35, 255),
	}

	ThemeYellow = CardTheme{
		Name:        "Yellow",
		Background:  widget.RGBA8(255, 238, 125, 135), // Airy translucent sunny
		Foreground:  widget.RGBA8(56, 42, 4, 255),
		Secondary:   widget.RGBA8(88, 68, 12, 220),
		Border:      widget.RGBA8(255, 255, 255, 140),
		AccentPill:  widget.RGBA8(240, 220, 100, 160),
		AccentPillT: widget.RGBA8(50, 38, 5, 255),
	}

	ThemeSky = CardTheme{
		Name:        "Sky",
		Background:  widget.RGBA8(165, 218, 252, 135), // Airy translucent sky
		Foreground:  widget.RGBA8(8, 42, 68, 255),
		Secondary:   widget.RGBA8(20, 68, 105, 220),
		Border:      widget.RGBA8(255, 255, 255, 140),
		AccentPill:  widget.RGBA8(140, 198, 240, 160),
		AccentPillT: widget.RGBA8(8, 40, 65, 255),
	}

	ThemeLavender = CardTheme{
		Name:        "Lavender",
		Background:  widget.RGBA8(220, 200, 252, 135), // Airy translucent lilac
		Foreground:  widget.RGBA8(42, 20, 68, 255),
		Secondary:   widget.RGBA8(68, 38, 100, 220),
		Border:      widget.RGBA8(255, 255, 255, 140),
		AccentPill:  widget.RGBA8(195, 170, 245, 160),
		AccentPillT: widget.RGBA8(38, 15, 62, 255),
	}

	ThemePeach = CardTheme{
		Name:        "Peach",
		Background:  widget.RGBA8(255, 205, 185, 135), // Airy translucent coral
		Foreground:  widget.RGBA8(68, 28, 12, 255),
		Secondary:   widget.RGBA8(105, 48, 24, 220),
		Border:      widget.RGBA8(255, 255, 255, 140),
		AccentPill:  widget.RGBA8(245, 175, 150, 160),
		AccentPillT: widget.RGBA8(60, 22, 8, 255),
	}

	ThemeObsidian = CardTheme{
		Name:        "Obsidian",
		Background:  widget.RGBA8(22, 28, 42, 155), // Translucent frosted dark glass
		Foreground:  widget.RGBA8(240, 246, 255, 255),
		Secondary:   widget.RGBA8(148, 163, 184, 255),
		Border:      widget.RGBA8(255, 255, 255, 60),
		AccentPill:  widget.RGBA8(36, 46, 68, 180),
		AccentPillT: widget.RGBA8(220, 235, 255, 255),
	}
)

var AvailableThemes = []CardTheme{
	ThemeMint,
	ThemeYellow,
	ThemeSky,
	ThemeLavender,
	ThemePeach,
	ThemeObsidian,
}

// GetTicketTheme deterministically assigns an airy pastel theme by index
func GetTicketTheme(idx int) CardTheme {
	themes := []CardTheme{ThemeMint, ThemeYellow, ThemeSky, ThemeLavender, ThemePeach}
	return themes[idx%len(themes)]
}

// Global translucent UI Tokens
var (
	ColorGlassBg      = widget.RGBA8(20, 26, 40, 140)
	ColorGlassBgDeep  = widget.RGBA8(15, 20, 32, 180)
	ColorGlassBorder  = widget.RGBA8(255, 255, 255, 55)
	ColorGlassCard    = widget.RGBA8(26, 34, 52, 160)
	ColorGlassCardHov = widget.RGBA8(38, 48, 72, 190)
	ColorGlassCardAct = widget.RGBA8(50, 64, 94, 220)
	ColorBorderSubtle = widget.RGBA8(255, 255, 255, 40)
	ColorBorderHover  = widget.RGBA8(255, 255, 255, 120)
	ColorBorderAccent = widget.RGBA8(59, 130, 246, 255)
	ColorGlowTop      = widget.RGBA8(255, 255, 255, 140)

	ColorCardBackdrop = widget.RGBA8(20, 24, 35, 235)
	ColorInputBg      = widget.RGBA8(15, 20, 30, 220)
	ColorInputFocused = widget.RGBA8(24, 32, 48, 240)

	ColorTextPrimary   = widget.RGBA8(245, 250, 255, 255)
	ColorTextSecondary = widget.RGBA8(148, 163, 184, 255)
	ColorTextMuted     = widget.RGBA8(100, 116, 139, 255)

	ColorPillBg      = widget.RGBA8(20, 26, 40, 140)
	ColorPillBgHover = widget.RGBA8(38, 48, 72, 190)
	ColorPillBorder  = widget.RGBA8(255, 255, 255, 60)

	ColorStatusToDo           = widget.RGBA8(59, 130, 246, 255)
	ColorStatusInProgress     = widget.RGBA8(245, 158, 11, 255)
	ColorStatusInProgressGlow = widget.RGBA8(245, 158, 11, 80)
	ColorStatusInReview       = widget.RGBA8(168, 85, 247, 255)
	ColorStatusInReviewGlow   = widget.RGBA8(168, 85, 247, 80)
	ColorStatusDone           = widget.RGBA8(34, 197, 94, 255)
	ColorStatusDoneGlow       = widget.RGBA8(34, 197, 94, 80)
	ColorStatusUnknown        = widget.RGBA8(148, 163, 184, 255)

	ColorPriorityHighest = widget.RGBA8(239, 68, 68, 255)
	ColorPriorityHigh    = widget.RGBA8(249, 115, 22, 255)
	ColorPriorityMedium  = widget.RGBA8(234, 179, 8, 255)
	ColorPriorityLow     = widget.RGBA8(59, 130, 246, 255)
	ColorPriorityLowest  = widget.RGBA8(148, 163, 184, 255)

	ColorToastBg     = widget.RGBA8(15, 23, 42, 240)
	ColorToastBorder = widget.RGBA8(59, 130, 246, 180)
)

// GetStatusColors returns foreground, background, and glow colors for a status category and name
func GetStatusColors(categoryKey, statusName string) (fg, bg, glow widget.Color) {
	nameLower := strings.ToLower(statusName)
	if strings.Contains(nameLower, "review") {
		return ColorStatusInReview, widget.RGBA8(58, 28, 88, 160), ColorStatusInReviewGlow
	}

	switch categoryKey {
	case "new", "to_do":
		return widget.RGBA8(147, 197, 253, 255), widget.RGBA8(30, 58, 138, 140), widget.RGBA8(59, 130, 246, 80)
	case "indeterminate", "in_progress":
		return widget.RGBA8(253, 230, 138, 255), widget.RGBA8(120, 53, 15, 140), ColorStatusInProgressGlow
	case "done":
		return widget.RGBA8(167, 243, 208, 255), widget.RGBA8(6, 78, 59, 140), ColorStatusDoneGlow
	default:
		return widget.RGBA8(226, 232, 240, 255), widget.RGBA8(51, 65, 85, 140), widget.RGBA8(148, 163, 184, 80)
	}
}

// ProfileColorPreset provides a curated palette for custom Jira profiles
type ProfileColorPreset struct {
	Name  string
	Hex   string
	Color widget.Color
	Glow  widget.Color
}

var ProfilePresets = []ProfileColorPreset{
	{Name: "Cyan", Hex: "#38bdf8", Color: widget.RGBA8(56, 189, 248, 255), Glow: widget.RGBA8(56, 189, 248, 65)},
	{Name: "Purple", Hex: "#a855f7", Color: widget.RGBA8(168, 85, 247, 255), Glow: widget.RGBA8(168, 85, 247, 65)},
	{Name: "Amber", Hex: "#eab308", Color: widget.RGBA8(234, 179, 8, 255), Glow: widget.RGBA8(234, 179, 8, 65)},
	{Name: "Emerald", Hex: "#22c55e", Color: widget.RGBA8(34, 197, 94, 255), Glow: widget.RGBA8(34, 197, 94, 65)},
	{Name: "Pink", Hex: "#ec4899", Color: widget.RGBA8(236, 72, 153, 255), Glow: widget.RGBA8(236, 72, 153, 65)},
	{Name: "Orange", Hex: "#f97316", Color: widget.RGBA8(249, 115, 22, 255), Glow: widget.RGBA8(249, 115, 22, 65)},
	{Name: "Blue", Hex: "#3b82f6", Color: widget.RGBA8(59, 130, 246, 255), Glow: widget.RGBA8(59, 130, 246, 65)},
	{Name: "Rose", Hex: "#f43f5e", Color: widget.RGBA8(244, 63, 94, 255), Glow: widget.RGBA8(244, 63, 94, 65)},
}

// ParseHexColor parses a CSS hex color string (e.g., #38bdf8) into a widget.Color
func ParseHexColor(hexStr string, fallback widget.Color) widget.Color {
	hex := strings.TrimPrefix(strings.TrimSpace(hexStr), "#")
	if len(hex) == 6 {
		var r, g, b uint8
		for i := 0; i < 3; i++ {
			byteVal := byte(0)
			for j := 0; j < 2; j++ {
				c := hex[i*2+j]
				byteVal <<= 4
				if c >= '0' && c <= '9' {
					byteVal += c - '0'
				} else if c >= 'a' && c <= 'f' {
					byteVal += c - 'a' + 10
				} else if c >= 'A' && c <= 'F' {
					byteVal += c - 'A' + 10
				}
			}
			if i == 0 {
				r = byteVal
			} else if i == 1 {
				g = byteVal
			} else {
				b = byteVal
			}
		}
		return widget.RGBA8(r, g, b, 255)
	}
	return fallback
}

// GetInstanceColors calculates full theme palette for an instance (using its custom profile color if set)
func GetInstanceColors(colorHex string, idx int) (core widget.Color, glow widget.Color, inProg widget.Color, todo widget.Color, track widget.Color, badgeBg widget.Color, badgeBorder widget.Color) {
	defaultPreset := ProfilePresets[idx%len(ProfilePresets)]
	core = defaultPreset.Color
	if colorHex != "" {
		core = ParseHexColor(colorHex, defaultPreset.Color)
	}

	glow = widget.RGBA(core.R, core.G, core.B, 0.25)
	inProg = core
	todo = widget.RGBA(core.R*0.6, core.G*0.6, core.B*0.6, 1.0)
	track = widget.RGBA(core.R, core.G, core.B, 0.15)
	badgeBg = widget.RGBA(core.R*0.12+0.05, core.G*0.12+0.05, core.B*0.12+0.08, 0.92)
	badgeBorder = widget.RGBA(core.R, core.G, core.B, 0.55)
	return
}

