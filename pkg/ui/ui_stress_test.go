package ui

import (
	"image"
	"runtime"
	"sync"
	"testing"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

type mockCanvas struct{}

func (m *mockCanvas) Clear(color widget.Color)                                               {}
func (m *mockCanvas) DrawRect(r geometry.Rect, color widget.Color)                           {}
func (m *mockCanvas) FillRectDirect(r geometry.Rect, color widget.Color)                     {}
func (m *mockCanvas) StrokeRect(r geometry.Rect, color widget.Color, strokeWidth float32)    {}
func (m *mockCanvas) DrawRoundRect(r geometry.Rect, color widget.Color, radius float32)     {}
func (m *mockCanvas) StrokeRoundRect(r geometry.Rect, color widget.Color, radius float32, strokeWidth float32) {
}
func (m *mockCanvas) DrawCircle(center geometry.Point, radius float32, color widget.Color) {}
func (m *mockCanvas) StrokeCircle(center geometry.Point, radius float32, color widget.Color, strokeWidth float32) {
}
func (m *mockCanvas) StrokeArc(center geometry.Point, radius float32, startAngle, sweepAngle float64, color widget.Color, strokeWidth float32) {
}
func (m *mockCanvas) DrawLine(from, to geometry.Point, color widget.Color, strokeWidth float32) {}
func (m *mockCanvas) DrawText(text string, bounds geometry.Rect, fontSize float32, color widget.Color, bold bool, align widget.TextAlign) {
}
func (m *mockCanvas) MeasureText(text string, fontSize float32, bold bool) float32 {
	return float32(len(text)) * fontSize * 0.6
}
func (m *mockCanvas) DrawImage(img image.Image, at geometry.Point)   {}
func (m *mockCanvas) PushClip(r geometry.Rect)                       {}
func (m *mockCanvas) PushClipRoundRect(r geometry.Rect, radius float32) {}
func (m *mockCanvas) PopClip()                                       {}
func (m *mockCanvas) PushTransform(offset geometry.Point)            {}
func (m *mockCanvas) PopTransform()                                  {}
func (m *mockCanvas) TransformOffset() geometry.Point                { return geometry.Pt(0, 0) }
func (m *mockCanvas) ScreenOriginBase() geometry.Point               { return geometry.Pt(0, 0) }
func (m *mockCanvas) ClipBounds() geometry.Rect                      { return geometry.NewRect(0, 0, 1000, 1000) }
func (m *mockCanvas) ReplayScene(s widget.SceneCache)                {}

type mockContext struct{}

func (c *mockContext) RequestFocus(w widget.Widget)          {}
func (c *mockContext) ReleaseFocus(w widget.Widget)          {}
func (c *mockContext) IsFocused(w widget.Widget) bool        { return false }
func (c *mockContext) FocusedWidget() widget.Widget          { return nil }
func (c *mockContext) Now() time.Time                        { return time.Now() }
func (c *mockContext) DeltaTime() time.Duration              { return 16 * time.Millisecond }
func (c *mockContext) Invalidate()                           {}
func (c *mockContext) InvalidateRect(r geometry.Rect)        {}
func (c *mockContext) Cursor() widget.CursorType             { return 0 }
func (c *mockContext) SetCursor(cursor widget.CursorType)    {}
func (c *mockContext) Scale() float32                        { return 1.0 }
func (c *mockContext) ThemeProvider() widget.ThemeProvider   { return nil }
func (c *mockContext) OverlayManager() widget.OverlayManager { return nil }
func (c *mockContext) WindowSize() geometry.Size             { return geometry.Sz(800, 600) }
func (c *mockContext) Scheduler() widget.SchedulerRef        { return nil }

func setupStressAppView() (*AppView, *jira.Client) {
	cfg := jira.Config{
		DemoMode: true,
		Instances: []jira.InstanceConfig{
			{ID: "inst-1", Name: "Cloud A", BaseURL: "https://a.test", Email: "a@test.com", APIToken: "tok-a"},
			{ID: "inst-2", Name: "Cloud B", BaseURL: "https://b.test", Email: "b@test.com", APIToken: "tok-b"},
			{ID: "inst-3", Name: "Cloud C", BaseURL: "https://c.test", Email: "c@test.com", APIToken: "tok-c"},
		},
	}
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, func() {})
	return view, client
}

// TestAppViewParallelStress executes concurrent Draw, handleKey, handleClick, handleHover,
// RefreshIssues, SetState, Layout, OpenSettings, and saveSettings under 50 parallel goroutines.
func TestAppViewParallelStress(t *testing.T) {
	view, _ := setupStressAppView()
	defer view.Close()

	ctx := &mockContext{}
	canvas := &mockCanvas{}

	const numWorkers = 50
	const iterations = 60
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		workerID := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch workerID % 10 {
				case 0:
					// Concurrent Draw
					view.Draw(ctx, canvas)
				case 1:
					// Concurrent Key Events
					key := event.KeyA
					if i%2 == 0 {
						key = event.KeyBackspace
					} else if i%3 == 0 {
						key = event.KeyEscape
					}
					ev := event.NewKeyEvent(event.KeyPress, key, rune('a'+(i%26)), event.ModNone)
					view.Event(ctx, ev)
					view.handleKey(ev)
				case 2:
					// Key events with modifiers (Cmd+C, Cmd+V, Cmd+A)
					modEv := event.NewKeyEvent(event.KeyPress, event.KeyC, 'c', event.Modifiers(1))
					view.handleKey(modEv)
					pasteEv := event.NewKeyEvent(event.KeyPress, event.KeyV, 'v', event.Modifiers(1))
					view.handleKey(pasteEv)
				case 3:
					// Concurrent Mouse Clicks
					clickPos := geometry.Pt(float32(10+(i%100)), float32(20+(i%200)))
					view.handleClick(clickPos)
					mouseEv := event.NewMouseEvent(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, clickPos, clickPos, event.ModNone)
					view.Event(ctx, mouseEv)
				case 4:
					// Concurrent Mouse Hover
					hoverPos := geometry.Pt(float32(5+(i%50)), float32(10+(i%150)))
					view.handleHover(hoverPos)
					moveEv := event.NewMouseEvent(event.MouseMove, event.ButtonNone, event.ButtonState(0), hoverPos, hoverPos, event.ModNone)
					view.Event(ctx, moveEv)
				case 5:
					// Concurrent State Transitions
					st := window.WindowState(i % 3)
					view.SetState(st)
					view.Layout(ctx, geometry.Loose(geometry.Sz(800, 600)))
				case 6:
					// Concurrent Refresh & Settings Toggle
					if i%2 == 0 {
						view.RefreshIssues()
					} else {
						view.ToggleSettings()
					}
				case 7:
					// Snapshots & Filtered Issues
					_ = view.snapshot()
					_ = view.getFilteredIssues()
				case 8:
					// Expand & Toast
					view.Expand(i % 5)
					view.showToast("Stress notification")
				case 9:
					// Wheel scroll
					wheelEv := event.NewWheelEvent(geometry.Pt(0, float32((i%5)*10)), geometry.Pt(10, 10), geometry.Pt(10, 10), event.ModNone)
					view.Event(ctx, wheelEv)
				}
			}
		}()
	}

	wg.Wait()
}

// TestAppViewConcurrentCloseDuringActivity ensures calling Close while goroutines are actively
// interacting does not deadlock or panic.
func TestAppViewConcurrentCloseDuringActivity(t *testing.T) {
	for repeat := 0; repeat < 5; repeat++ {
		view, _ := setupStressAppView()
		ctx := &mockContext{}
		canvas := &mockCanvas{}

		stop := make(chan struct{})
		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						view.Draw(ctx, canvas)
						view.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyA, 'a', event.ModNone))
						view.handleClick(geometry.Pt(20, 20))
						view.handleHover(geometry.Pt(15, 15))
						view.RefreshIssues()
					}
				}
			}()
		}

		time.Sleep(20 * time.Millisecond)
		view.Close()
		close(stop)
		wg.Wait()
	}
}

// TestAppViewGoroutineLifecycle ensures no background goroutine leaks across repeated AppView lifecycles.
func TestAppViewGoroutineLifecycle(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		view, _ := setupStressAppView()
		view.SetState(window.StateFan)
		view.handleHover(geometry.Pt(16, 50))
		view.showToast("Testing")
		time.Sleep(5 * time.Millisecond)
		view.Close()
	}

	time.Sleep(200 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()

	if finalGoroutines > initialGoroutines+5 {
		t.Errorf("AppView goroutine leak detected: initial=%d, final=%d", initialGoroutines, finalGoroutines)
	}
}

// TestSettingsWidgetConcurrentTestConnectionAndDraw tests concurrent connection testing and drawing
func TestSettingsWidgetConcurrentTestConnectionAndDraw(t *testing.T) {
	cfg := jira.Config{
		DemoMode: true,
		Instances: []jira.InstanceConfig{
			{ID: "inst-1", Name: "Cloud A", BaseURL: "https://a.test", Email: "a@test.com", APIToken: "tok-a"},
		},
	}
	settings := NewSettingsWidget(cfg, nil, nil)
	ctx := &mockContext{}
	canvas := &mockCanvas{}

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: testConnection
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			settings.testConnection()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 2: Draw
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			settings.Draw(ctx, canvas)
			time.Sleep(500 * time.Microsecond)
		}
	}()

	wg.Wait()
}

