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
)

func main() {
	// 1. Load multi-tenant configuration (or default fallback with .env)
	cfg := jira.LoadConfig()

	// 2. Initialize Jira multi-tenant REST client
	client := jira.NewClient(cfg)

	// 3. Initialize Gogpu engine with Native Transparency & Frameless styling
	gogpuApp := gogpu.NewApp(gogpu.Config{
		Title:       "",
		Width:       26,
		Height:      210,
		Frameless:   true,
		Transparent: true,
	})

	// 4. Create UI Application connected to the GPU Window & Event Pipeline
	uiApp := app.New(
		app.WithWindowProvider(gogpuApp),
		app.WithPlatformProvider(gogpuApp),
		app.WithEventSource(gogpuApp.EventSource()),
		app.WithRenderMode(app.RenderModeFrameworkManaged),
	)

	// 5. Initialize the Edge Rail HUD Root View with resize & redraw hooks
	rootView := ui.NewAppView(
		cfg,
		client,
		func() {
			gogpuApp.RequestRedraw()
		},
		func(w, h int) {
			gogpuApp.RequestSize(w, h)
			gogpuApp.RequestRedraw()
		},
	)

	uiApp.SetRoot(rootView)

	// 6. Background poller & edge dock positioning
	go func() {
		for _, delay := range []time.Duration{80 * time.Millisecond, 250 * time.Millisecond, 600 * time.Millisecond} {
			time.Sleep(delay)
			if window.DefaultManager != nil {
				_ = window.DefaultManager.InitEdgeRail(26, 210)
			}
			gogpuApp.RequestSize(26, 210)
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

	// 7. Run GPU desktop pipeline
	if err := desktop.Run(gogpuApp, uiApp); err != nil {
		log.Fatalf("Fatal error running desktop app: %v", err)
	}
}
