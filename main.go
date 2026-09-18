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

var (
	version   = "1.0.0"
	buildTime = "unknown"
)

func main() {
	// 1. Load multi-tenant configuration (or default fallback with .env)
	cfg := jira.LoadConfig()

	// 2. Initialize Jira multi-tenant REST client
	client := jira.NewClient(cfg)

	// 3. Initialize Gogpu engine with macOS Dock dimensions (36x224)
	gogpuApp := gogpu.NewApp(gogpu.Config{
		Title:  "",
		Width:  36,
		Height: 224,
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
	rootView.SetOnResize(func(w, h int) {
		uiApp.Window().HandleResize(w, h)
		gogpuApp.RequestRedraw()
	})
	defer rootView.Close()

	reloadCh := make(chan struct{}, 1)
	rootView.SetOnConfigReload(func() {
		select {
		case reloadCh <- struct{}{}:
		default:
		}
	})

	// Initial issue & status fetch
	rootView.RefreshIssues()
	if cfg.StatusCheckEnabled {
		rootView.RefreshStatus()
	}

	uiApp.SetRoot(rootView)

	// 7. Dynamic background poller & edge dock positioning
	go func() {
		for _, delay := range []time.Duration{60 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond} {
			time.Sleep(delay)
			if window.DefaultManager != nil {
				window.DefaultManager.SetDockSide(window.DockSide(cfg.DockSide))
				_ = window.DefaultManager.InitEdgeRail(36, 224)
			}
			gogpuApp.RequestRedraw()
		}

		pollInterval := cfg.PollInterval
		if pollInterval <= 0 {
			pollInterval = 300
		}
		statInterval := cfg.StatusPollInterval
		if statInterval <= 0 {
			statInterval = 300
		}

		issueTicker := time.NewTicker(time.Duration(pollInterval) * time.Second)
		defer issueTicker.Stop()

		statusTicker := time.NewTicker(time.Duration(statInterval) * time.Second)
		defer statusTicker.Stop()

		for {
			select {
			case <-issueTicker.C:
				rootView.RefreshIssues()
				gogpuApp.RequestRedraw()
			case <-statusTicker.C:
				latestCfg := jira.LoadConfig()
				if latestCfg.StatusCheckEnabled {
					rootView.RefreshStatus()
					gogpuApp.RequestRedraw()
				}
			case <-reloadCh:
				latestCfg := jira.LoadConfig()
				newPoll := latestCfg.PollInterval
				if newPoll <= 0 {
					newPoll = 300
				}
				newStat := latestCfg.StatusPollInterval
				if newStat <= 0 {
					newStat = 300
				}
				issueTicker.Reset(time.Duration(newPoll) * time.Second)
				statusTicker.Reset(time.Duration(newStat) * time.Second)
				if latestCfg.StatusCheckEnabled {
					rootView.RefreshStatus()
				}
				gogpuApp.RequestRedraw()
			}
		}
	}()

	fmt.Printf("🚀 Jira Quick Access v%s (built %s) [macOS Dock Rail HUD] running...\n", version, buildTime)

	// 8. Run GPU desktop pipeline
	if err := desktop.Run(gogpuApp, uiApp); err != nil {
		log.Fatalf("Fatal error running desktop app: %v", err)
	}
}
