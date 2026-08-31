//go:build darwin

package window

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

static NSWindow *g_appWindow = nil;
static WKWebView *g_webView = nil;

static void ApplyDarwinWindowStyles(NSWindow *window) {
    if (!window) return;

    [window setAcceptsMouseMovedEvents:YES];
    [window setOpaque:NO];
    [window setBackgroundColor:[NSColor clearColor]];
    [window setHasShadow:NO];

    // Completely borderless and transparent without any titlebar artifacts
    [window setStyleMask:(NSWindowStyleMaskBorderless | NSWindowStyleMaskFullSizeContentView)];
    [window setTitleVisibility:NSWindowTitleHidden];
    [window setTitlebarAppearsTransparent:YES];

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
        contentView.layer.backgroundColor = [[NSColor clearColor] CGColor];
        contentView.layer.opaque = NO;

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
            for (NSWindow *w in windows) {
                if ([w isVisible] || [windows count] == 1) {
                    g_appWindow = w;
                    break;
                }
            }
        }
        if (!g_appWindow) return;

        ApplyDarwinWindowStyles(g_appWindow);

        NSScreen *targetScreen = [g_appWindow screen];
        if (!targetScreen) {
            targetScreen = [NSScreen mainScreen];
        }

        NSRect screenFrame = [targetScreen frame];
        CGFloat x = screenFrame.origin.x + screenFrame.size.width - (CGFloat)width;
        CGFloat y = screenFrame.origin.y + (screenFrame.size.height - (CGFloat)height) / 2.0;

        NSRect frame = NSMakeRect(x, y, (CGFloat)width, (CGFloat)height);
        [g_appWindow setFrame:frame display:YES animate:NO];
        [g_appWindow makeKeyAndOrderFront:nil];

        if (g_webView) {
            // StateExpanded = 2
            if (state == 2) {
                CGFloat webW = (CGFloat)width - 120.0;
                CGFloat webH = (CGFloat)height - 20.0;
                [g_webView setFrame:NSMakeRect(10, 10, webW, webH)];
                [g_webView setHidden:NO];
            } else {
                [g_webView setHidden:YES];
            }
        }
    });
}

static void DarwinInitMobileWebView() {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_appWindow) {
            NSArray *windows = [NSApp windows];
            for (NSWindow *w in windows) {
                if ([w isVisible] || [windows count] == 1) {
                    g_appWindow = w;
                    break;
                }
            }
        }
        if (!g_appWindow || g_webView) return;

        WKWebViewConfiguration *config = [[WKWebViewConfiguration alloc] init];
        g_webView = [[WKWebView alloc] initWithFrame:NSMakeRect(10, 10, 600, 500) configuration:config];
        [g_webView setHidden:YES];
        [g_webView setValue:@NO forKey:@"drawsBackground"];

        [[g_appWindow contentView] addSubview:g_webView];
    });
}

static void DarwinSetWebViewVisible(int visible, int width, int height) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_webView) return;
        if (visible) {
            CGFloat webW = (CGFloat)width - 120.0;
            CGFloat webH = (CGFloat)height - 20.0;
            [g_webView setFrame:NSMakeRect(10, 10, webW, webH)];
            [g_webView setHidden:NO];
        } else {
            [g_webView setHidden:YES];
        }
    });
}

static void DarwinLoadWebViewURL(const char *urlStr) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_webView) return;
        NSString *nsUrlStr = [NSString stringWithUTF8String:urlStr];
        NSURL *url = [NSURL URLWithString:nsUrlStr];
        if (url) {
            NSURLRequest *req = [NSURLRequest requestWithURL:url];
            [g_webView loadRequest:req];
        }
    });
}

static void DarwinLoadWebViewHTML(const char *htmlStr, const char *baseURLStr) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_webView) return;
        NSString *nsHtml = [NSString stringWithUTF8String:htmlStr];
        NSURL *baseURL = nil;
        if (baseURLStr && strlen(baseURLStr) > 0) {
            baseURL = [NSURL URLWithString:[NSString stringWithUTF8String:baseURLStr]];
        }
        [g_webView loadHTMLString:nsHtml baseURL:baseURL];
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
	DefaultManager = &DarwinManager{
		state: StateRest,
	}
}

func (m *DarwinManager) InitEdgeRail(width, height int) error {
	C.DarwinInitMobileWebView()
	C.DarwinDockToRightEdge(C.int(width), C.int(height), C.int(0))
	return nil
}

func (m *DarwinManager) SetState(state WindowState, width, height int) {
	m.state = state
	C.DarwinDockToRightEdge(C.int(width), C.int(height), C.int(state))
}

func (m *DarwinManager) DockToRightEdge(width, height int) {
	C.DarwinDockToRightEdge(C.int(width), C.int(height), C.int(m.state))
}

func (m *DarwinManager) OpenTicketURL(url string) error {
	LoadMobileTicketView(url)
	return nil
}

func SetMobileWebViewVisible(visible bool, width, height int) {
	v := 0
	if visible {
		v = 1
	}
	C.DarwinSetWebViewVisible(C.int(v), C.int(width), C.int(height))
}

func LoadMobileTicketView(url string) {
	cStr := C.CString(url)
	defer C.free(unsafe.Pointer(cStr))
	C.DarwinLoadWebViewURL(cStr)
}

func LoadMobileTicketHTML(html string, baseURL string) {
	cHtml := C.CString(html)
	defer C.free(unsafe.Pointer(cHtml))
	cBase := C.CString(baseURL)
	defer C.free(unsafe.Pointer(cBase))
	C.DarwinLoadWebViewHTML(cHtml, cBase)
}
