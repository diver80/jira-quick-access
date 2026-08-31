//go:build darwin && cgo

package window

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework QuartzCore
#include <Cocoa/Cocoa.h>
#include <WebKit/WebKit.h>
#include <QuartzCore/QuartzCore.h>
#include <dispatch/dispatch.h>

// Allow borderless HUD window to become key and receive text input
@interface NSWindow (AllowKeyWindow)
@end

@implementation NSWindow (AllowKeyWindow)
- (BOOL)canBecomeKeyWindow {
    return YES;
}
- (BOOL)canBecomeMainWindow {
    return YES;
}
@end

static NSWindow *g_appWindow = nil;
static WKWebView *g_ticketWebView = nil;

static void ApplyDarwinWindowStyles(NSWindow *window) {
    if (!window) return;

    [window setAcceptsMouseMovedEvents:YES];
    [window setOpaque:NO];
    [window setBackgroundColor:[NSColor clearColor]];
    [window setHasShadow:NO];

    [window setTitleVisibility:NSWindowTitleHidden];
    [window setTitlebarAppearsTransparent:YES];
    [window setStyleMask:([window styleMask] | NSWindowStyleMaskFullSizeContentView)];

    NSButton *closeBtn = [window standardWindowButton:NSWindowCloseButton];
    if (closeBtn) [closeBtn setHidden:YES];
    NSButton *minBtn = [window standardWindowButton:NSWindowMiniaturizeButton];
    if (minBtn) [minBtn setHidden:YES];
    NSButton *zoomBtn = [window standardWindowButton:NSWindowZoomButton];
    if (zoomBtn) [zoomBtn setHidden:YES];

    NSWindowCollectionBehavior behavior =
        NSWindowCollectionBehaviorCanJoinAllSpaces |
        NSWindowCollectionBehaviorStationary |
        NSWindowCollectionBehaviorIgnoresCycle |
        NSWindowCollectionBehaviorFullScreenAuxiliary;
    [window setCollectionBehavior:behavior];

    [window setLevel:NSFloatingWindowLevel];

    NSView *contentView = [window contentView];
    if (contentView) {
        [contentView setWantsLayer:YES];
        contentView.layer.opaque = NO;
        contentView.layer.backgroundColor = [[NSColor clearColor] CGColor];

        for (CALayer *layer in contentView.layer.sublayers) {
            layer.opaque = NO;
            layer.backgroundColor = [[NSColor clearColor] CGColor];
        }

        NSTrackingArea *trackingArea = [[NSTrackingArea alloc] initWithRect:[contentView bounds]
            options:(NSTrackingMouseMoved | NSTrackingMouseEnteredAndExited | NSTrackingActiveAlways | NSTrackingInVisibleRect)
            owner:contentView
            userInfo:nil];
        [contentView addTrackingArea:trackingArea];
    }
}

static void DarwinDockToRightEdge(int width, int height, int state) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_appWindow) {
            NSArray *windows = [NSApp windows];
            if ([windows count] > 0) {
                g_appWindow = [windows objectAtIndex:0];
                ApplyDarwinWindowStyles(g_appWindow);
            }
        }
        if (!g_appWindow) return;

        ApplyDarwinWindowStyles(g_appWindow);

        NSScreen *screen = [g_appWindow screen];
        if (!screen) {
            screen = [NSScreen mainScreen];
        }
        if (!screen && [[NSScreen screens] count] > 0) {
            screen = [[NSScreen screens] objectAtIndex:0];
        }

        NSRect screenFrame = NSMakeRect(0, 0, 1440, 900);
        if (screen) {
            screenFrame = [screen visibleFrame];
            if (screenFrame.size.width <= 0 || screenFrame.size.height <= 0) {
                screenFrame = [screen frame];
            }
        }

        CGFloat x = screenFrame.origin.x + screenFrame.size.width - (CGFloat)width;
        CGFloat y = screenFrame.origin.y + (screenFrame.size.height - (CGFloat)height) / 2.0;

        NSRect frame = NSMakeRect(x, y, (CGFloat)width, (CGFloat)height);
        [g_appWindow setFrame:frame display:YES animate:NO];
        [g_appWindow makeKeyAndOrderFront:nil];
    });
}

static void DarwinSetMobileWebViewVisible(int visible, int w, int h) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_appWindow) {
            NSArray *windows = [NSApp windows];
            if ([windows count] > 0) {
                g_appWindow = [windows objectAtIndex:0];
            }
        }

        if (visible != 0) {
            if (!g_ticketWebView && g_appWindow) {
                NSView *contentView = [g_appWindow contentView];
                if (contentView) {
                    WKWebViewConfiguration *config = [[WKWebViewConfiguration alloc] init];
                    g_ticketWebView = [[WKWebView alloc] initWithFrame:NSMakeRect(8, 8, (CGFloat)(w-118), (CGFloat)(h-16)) configuration:config];
                    [g_ticketWebView setWantsLayer:YES];
                    [g_ticketWebView.layer setCornerRadius:14.0];
                    [g_ticketWebView.layer setMasksToBounds:YES];
                    [contentView addSubview:g_ticketWebView];
                }
            }
            if (g_ticketWebView) {
                CGFloat cardW = (CGFloat)(w - 118);
                CGFloat cardH = (CGFloat)(h - 16);
                [g_ticketWebView setFrame:NSMakeRect(8, 8, cardW, cardH)];
                [g_ticketWebView setHidden:NO];
            }
            if (g_appWindow) {
                [g_appWindow makeKeyAndOrderFront:nil];
            }
        } else {
            if (g_ticketWebView) {
                [g_ticketWebView setHidden:YES];
            }
        }
    });
}

static void DarwinLoadMobileTicketView(const char *urlStr) {
    if (!urlStr) return;
    NSString *nsStr = [NSString stringWithUTF8String:urlStr];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_ticketWebView) return;
        NSURL *url = [NSURL URLWithString:nsStr];
        if (url) {
            NSURLRequest *req = [NSURLRequest requestWithURL:url];
            [g_ticketWebView loadRequest:req];
        }
    });
}

static void DarwinLoadMobileTicketHTML(const char *htmlStr, const char *baseURLStr) {
    if (!htmlStr) return;
    NSString *nsHtml = [NSString stringWithUTF8String:htmlStr];
    NSString *nsBase = baseURLStr ? [NSString stringWithUTF8String:baseURLStr] : nil;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_ticketWebView) return;
        NSURL *baseURL = nsBase ? [NSURL URLWithString:nsBase] : nil;
        [g_ticketWebView loadHTMLString:nsHtml baseURL:baseURL];
    });
}
*/
import "C"
import (
	"unsafe"
)

type DarwinManager struct {
	state WindowState
}

func init() {
	if DefaultManager == nil {
		DefaultManager = &DarwinManager{
			state: StateRest,
		}
	}
}

func (m *DarwinManager) InitEdgeRail(width, height int) error {
	m.SetState(StateRest, width, height)
	return nil
}

func (m *DarwinManager) SetState(state WindowState, width, height int) {
	m.state = state
	m.DockToRightEdge(width, height)
}

func (m *DarwinManager) DockToRightEdge(width, height int) {
	C.DarwinDockToRightEdge(C.int(width), C.int(height), C.int(m.state))
}

func (m *DarwinManager) OpenTicketURL(url string) error {
	return OpenURL(url)
}

func SetMobileWebViewVisible(visible bool, width, height int) {
	var v C.int = 0
	if visible {
		v = 1
	}
	C.DarwinSetMobileWebViewVisible(v, C.int(width), C.int(height))
}

func LoadMobileTicketView(url string) {
	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	C.DarwinLoadMobileTicketView(cURL)
}

func LoadMobileTicketHTML(html string, baseURL string) {
	cHTML := C.CString(html)
	defer C.free(unsafe.Pointer(cHTML))
	var cBase *C.char
	if baseURL != "" {
		cBase = C.CString(baseURL)
		defer C.free(unsafe.Pointer(cBase))
	}
	C.DarwinLoadMobileTicketHTML(cHTML, cBase)
}
