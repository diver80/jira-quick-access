package main

import (
	"fmt"
	"log"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/ui"
	"jira-quick-access/pkg/window"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/theme"
	"github.com/gogpu/ui/widget"
)

func main() {
	// 1. Load multi-tenant configuration (or default fallback with .env)
	cfg := jira.LoadConfig()

	// 2. Initialize Jira multi-tenant REST client
	client := jira.NewClient(cfg)

	// 3. Initialize Gogpu engine
	gogpuApp := gogpu.NewApp(gogpu.Config{
		Title:  "",
		Width:  26,
		Height: 210,
	})

	// 4. Custom transparent theme so UI canvas clears with alpha=0 (no white corners)
	transparentTheme := theme.DefaultDark()
	transparentTheme.Colors.Background = widget.RGBA8(0, 0, 0, 0)
	transparentTheme.Colors.Surface = widget.RGBA8(0, 0, 0, 0)

	// 5. Create UI Application connected to the GPU Window & Event Pipeline
	uiApp := app.New(
		app.WithWindowProvider(gogpuApp),
		app.WithPlatformProvider(gogpuApp),
		app.WithEventSource(gogpuApp.EventSource()),
		app.WithTheme(transparentTheme),
		app.WithRenderMode(app.RenderModeFrameworkManaged),
	)

	// 6. Initialize the Edge Rail HUD Root View
	rootView := ui.NewAppView(
		cfg,
		client,
		func() {
			gogpuApp.RequestRedraw()
		},
	)

	uiApp.SetRoot(rootView)

	// 7. Background poller & edge dock positioning
	go func() {
		for _, delay := range []time.Duration{80 * time.Millisecond, 250 * time.Millisecond, 600 * time.Millisecond} {
			time.Sleep(delay)
			if window.DefaultManager != nil {
				_ = window.DefaultManager.InitEdgeRail(26, 210)
			}
			gogpuApp.RequestRedraw()
		}

		ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			rootView.RefreshIssues()
			gogpuApp.RequestRedraw()
		}
	}()

	fmt.Println("🚀 Jira Quick Access (Edge Rail HUD) running with transparent GPU canvas...")

	// 8. Run GPU desktop pipeline
	if err := desktop.Run(gogpuApp, uiApp); err != nil {
		log.Fatalf("Fatal error running desktop app: %v", err)
	}
}
