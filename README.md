# 🚀 Jira Quick Access (gogpu/ui)

A GPU-accelerated, native desktop companion for Jira Cloud inspired by **@nullbytes00**'s liquid-glass morphism aesthetics (SuperIsland / SuperCmd). Built with **Pure Go** and [`github.com/gogpu/ui`](https://github.com/gogpu/ui) with **Zero CGO dependencies**.

---

## ✨ Key Features & Aesthetic

* **💎 Obsidian Liquid-Glass Morphism:**
  * Translucent frosted glass panel with 1px ambient highlight borders and subtle specular reflections.
  * Glowing status indicator halos:
    * 🟢 **In Progress** (`#10B981` Emerald)
    * 🟡 **In Review** (`#F59E0B` Amber)
    * 🔵 **To Do** (`#38BDF8` Sky Blue)
    * 🟣 **Done** (`#A855F7` Electric Purple)
* **📐 Adaptive Multi-State Form Factor:**
  * **Collapsed Rail (36px width):** Minimalist edge pill displaying settings gear, vertical glowing status dots for active tickets, and quick search trigger. Expands smoothly on hover (>150ms) or shortcut.
  * **Expanded Drawer Panel (360px width):** Full command-center HUD with ticket cards, filters, quick transition dropdowns, and clipboard helpers.
  * **Settings Modal Overlay:** Frosted configuration sheet for Jira Cloud Base URL, User Email, API Token, custom JQL query, polling intervals, and connection testing.
* **⚡ Keyboard-First & Fast Workflow:**
  * `Cmd+K` / Search bar: Instant fuzzy filter by ticket key, summary, or issue type.
  * Quick filter tabs: `All`, `In Progress`, `Pinned`.
  * `[📋 Key]`: Instant copy issue key (e.g. `PROJ-102`) to clipboard.
  * `[📋 Branch]`: Instant copy sanitized Git branch name (e.g. `feature/PROJ-102-implement-oauth-pkce-flow`).
  * `[↗]`: Open issue directly in browser.
  * `[📌]`: Pin critical tickets to the top.
  * `[ In Progress ▾ ]`: Dynamic workflow status transitions with optimistic UI updates.
* **🔄 Jira REST API v3 Client & Demo Mode:**
  * Supports Basic Auth with Atlassian API Token.
  * Background polling sync with configurable interval and error backoff.
  * Built-in rich interactive demo mode for instant offline showcase and testing.

---

## 🛠️ Multi-Platform Build Script (`build.sh`)

The application compiles natively across **macOS (Apple Silicon & Intel)**, **Windows (x86_64 & ARM64)**, and **Linux (x86_64 & ARM64)** with zero CGO dependencies.

### Build Everything:
```bash
./build.sh all
```

### Target-Specific Builds:
```bash
# macOS (ARM64, AMD64 & .app bundle + tar.gz)
./build.sh osx

# Windows (.exe & .zip)
./build.sh win

# Linux (ELF binary & tar.gz)
./build.sh lin
```

### Run Locally:
```bash
go run .
```

### Run Tests:
```bash
go test -v ./...
```

---

## 📁 Project Architecture

```
jira-quick-access/
├── main.go                  # Main entry point & gogpu/ui bridge loop
├── build.sh                 # Multi-platform build script (OSX/Win/Lin)
├── instructions.md          # Architecture & state machine specification
├── pkg/
│   ├── jira/
│   │   ├── models.go        # Issue, Status, Transition, and Config data models
│   │   ├── client.go        # Jira REST API v3 client & branch sanitization
│   │   ├── mock.go          # Mock dataset & interactive demo transitions
│   │   ├── storage.go       # Local config persistence (~/.jira-quick-access)
│   │   └── jira_test.go     # Unit tests for Jira logic
│   └── ui/
│       ├── theme.go         # Liquid-glass tokens & status color palettes
│       ├── components.go    # IssueCard, StatusDot, GlassButton, Toast widgets
│       ├── rail_widget.go   # Collapsed 36px edge rail widget
│       ├── panel_widget.go  # Expanded 360px panel widget
│       ├── settings_widget.go # Glassmorphic settings overlay
│       ├── app_view.go      # Root state orchestrator & clipboard bridges
│       └── ui_test.go       # Layout & event unit tests
```
