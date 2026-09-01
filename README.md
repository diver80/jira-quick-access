# 🚀 Jira Quick Access

Jira tickets that live at the edge of your screen. A high-performance native desktop companion built in Go with [gogpu/ui](https://github.com/gogpu/ui) and native macOS WebKit.

No dock clutter, no window to manage. Slide the pointer to the right edge and the deck fans out.

**[Download for macOS (Universal App & DMG)](dist/osx/)** · **[Windows & Linux Binaries](dist/)**

---

| At rest | Fanned | A ticket pulled open |
|---|---|---|
| A 32 pt macOS Dock frosted capsule with instance beacons & proportional status gauges | Shingled pastel tabs with instant search, cycle headers & Dock-style hover lift | Full embedded ticket view in native WebKit with floating close button & side tabs |

| State | What you see | Trigger |
|---|---|---|
| **Rest** | A 32 pt discrete frosted glass capsule on the right screen edge — glowing beacons, live ticket counts, and proportional status breakdown gauges (To Do / In Progress / Done) per instance | idle |
| **Fan** | Ticket tabs shingle down the edge with instant search, multi-instance cycling, and animated Dock-style hover magnification | pointer enters the capsule |
| **Expanded** | The ticket slides open in full size via embedded hardware-accelerated WebKit, stripped of bloated Jira navigation headers, level with its own side shelf | click a tab |

---

## ✨ Features & Polish

### 🏝️ 3-State Edge Rail Architecture
- **Resting Capsule (32×224)**: Minimalist frosted glass capsule anchored to the screen edge. Features continuous 1.5px frosted white border stroke, instance beacon halos, centered count badges, and proportional gauge lines scaled against maximum workload.
- **Interactive Fan Deck (120×H)**: Vertical tabs cascade down the edge. Resting the mouse over a tab triggers an organic **macOS Dock lift effect** — sliding 4px to the left with a radiant glowing border and vibrant indicator pill.
- **Embedded WebKit Experience (780×580)**: Native macOS `WKWebView` renders the complete Jira issue directly on screen. Clean user script removes global Atlassian navigation headers, giving you pure issue content.

### 🌐 Multi-Instance Jira Support
- Manage multiple Jira Cloud & Data Center instances simultaneously (e.g. *Avono*, *Sandbox*, *Sandbox*).
- Color-coded instance beacons (Cyan, Purple, Amber, Emerald) and per-instance JQL filters.
- Cycle through active instances with a single click on the header pill in Fan mode.

### ⌨️ Native Shortcuts & Copy-Paste Support
Standard macOS clipboard shortcuts dispatch natively throughout embedded WebViews and input fields:

| Shortcut | Action |
|---|---|
| `⌘V` | Paste (API tokens, OTP / 2FA verification codes, text) |
| `⌘C` | Copy selected text or credentials |
| `⌘A` | Select all |
| `⌘Z` | Undo |
| `Esc` / `⌘W` | Close expanded ticket view back to tabs, or collapse Fan deck to Rest capsule |
| `Backspace` | Erase instant search query in Fan mode |
| `Tab` / `Enter` | Cycle next field in Settings overlay |

---

## 🔒 Security & Privacy

- **Local Storage Only**: Configurations and tokens are saved strictly to your local machine at `~/.jira-quick-access/config.json`.
- **Zero Telemetry**: No tracking SDKs, no external servers, no analytics.
- **Direct Atlassian API**: All requests travel directly between your machine and your configured Jira instances via TLS.
- **Token Masking**: API tokens are masked in the UI (`AT••••••••••••00`) to prevent accidental shoulder surfing.

---

## 🛠️ Multi-Platform Build & Run

Requires **Go 1.21+**. Zero external C libraries required on Windows and Linux; uses native Cocoa/WebKit on macOS.

### Quick Start:
```bash
# Run locally in development
go run .

# Run test suite
go test -v ./...
```

### Build Release Packages:
The bundled `build.sh` script automates cross-compilation across macOS, Windows, and Linux:

```bash
# Build all platforms
./build.sh all

# Platform-specific builds
./build.sh osx    # macOS (ARM64 & x86_64 App Bundle + tar.gz)
./build.sh win    # Windows (.exe & .zip)
./build.sh lin    # Linux (ELF binary & tar.gz)
```

Generated artifacts are placed in `dist/`:
- `dist/osx/Jira Quick Access.app`
- `dist/osx/JiraQuickAccess-macOS-arm64.tar.gz`
- `dist/win/jira-quick-access-windows-amd64.exe`
- `dist/lin/jira-quick-access-linux-amd64`

---

## 📁 Architecture & Layout

```
jira-quick-access/
├── main.go                     # Application entry point & window initialization
├── build.sh                    # Multi-platform build & packaging automation
├── pkg/
│   ├── jira/
│   │   ├── client.go           # Multi-instance Jira REST API v3 client
│   │   ├── models.go           # Issue, InstanceConfig, Status & Theme models
│   │   ├── storage.go          # Config serialization & .env loader
│   │   └── mock.go             # Demo mock data generator
│   ├── ui/
│   │   ├── app_view.go         # 3-State root view, mouse tracking & Dock rendering
│   │   ├── theme.go            # Glassmorphic pastel palettes & design tokens
│   │   ├── components.go       # Toast notifications & UI helpers
│   │   └── settings_widget.go  # Multi-instance credentials overlay
│   └── window/
│       ├── manager_darwin.go   # macOS Cocoa edge docking, WKWebView & Edit Menu
│       ├── manager_windows.go  # Windows edge rail manager
│       └── manager_fallback.go # Linux edge rail manager
```

---

## 📄 License

MIT License — see [LICENSE](LICENSE) for details.
