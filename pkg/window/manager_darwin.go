//go:build darwin && cgo

package window

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework QuartzCore
#include <Cocoa/Cocoa.h>
#include <WebKit/WebKit.h>
#include <QuartzCore/QuartzCore.h>
#include <dispatch/dispatch.h>

static NSWindow *g_appWindow = nil;
static WKWebView *g_ticketWebView = nil;

static void SetupDarwinEditMenu(void) {
    if ([NSApp mainMenu]) {
        for (NSMenuItem *item in [[NSApp mainMenu] itemArray]) {
            if ([[item title] isEqualToString:@"Edit"]) return;
        }
    }

    NSMenu *mainMenu = [NSApp mainMenu];
    if (!mainMenu) {
        mainMenu = [[NSMenu alloc] initWithTitle:@"MainMenu"];
        [NSApp setMainMenu:mainMenu];
    }

    NSMenuItem *editMenuItem = [[NSMenuItem alloc] initWithTitle:@"Edit" action:nil keyEquivalent:@""];
    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];

    [editMenu addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
    [editMenu addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"Z"];
    [editMenu addItem:[NSMenuItem separatorItem]];
    [editMenu addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
    [editMenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
    [editMenu addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
    [editMenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];

    [editMenuItem setSubmenu:editMenu];
    [mainMenu addItem:editMenuItem];
}

// Allow borderless HUD window to become key, receive text input, and handle Cmd+V/Cmd+C clipboard shortcuts
@interface NSWindow (AllowKeyWindow)
@end

@implementation NSWindow (AllowKeyWindow)
- (BOOL)canBecomeKeyWindow {
    return YES;
}
- (BOOL)canBecomeMainWindow {
    return YES;
}
- (BOOL)performKeyEquivalent:(NSEvent *)event {
    if (([event modifierFlags] & NSEventModifierFlagDeviceIndependentFlagsMask) == NSEventModifierFlagCommand) {
        NSString *chars = [event charactersIgnoringModifiers];
        if ([chars isEqualToString:@"v"]) {
            if ([NSApp sendAction:@selector(paste:) to:nil from:self]) return YES;
        } else if ([chars isEqualToString:@"c"]) {
            if ([NSApp sendAction:@selector(copy:) to:nil from:self]) return YES;
        } else if ([chars isEqualToString:@"x"]) {
            if ([NSApp sendAction:@selector(cut:) to:nil from:self]) return YES;
        } else if ([chars isEqualToString:@"a"]) {
            if ([NSApp sendAction:@selector(selectAll:) to:nil from:self]) return YES;
        } else if ([chars isEqualToString:@"z"]) {
            if ([NSApp sendAction:@selector(undo:) to:nil from:self]) return YES;
        }
    }
    return [super performKeyEquivalent:event];
}
@end

static void MakeLayersTransparent(CALayer *layer) {
    if (!layer) return;
    layer.opaque = NO;
    layer.backgroundColor = [[NSColor clearColor] CGColor];
    for (CALayer *sub in layer.sublayers) {
        MakeLayersTransparent(sub);
    }
}

static void ApplyDarwinWindowStyles(NSWindow *window) {
    if (!window) return;

    SetupDarwinEditMenu();

    // 1. Set style mask FIRST
    [window setStyleMask:NSWindowStyleMaskBorderless];

    // 2. Clear background and opacity on the NSWindow
    [window setAcceptsMouseMovedEvents:YES];
    [window setOpaque:NO];
    [window setBackgroundColor:[NSColor clearColor]];
    [window setHasShadow:NO];

    NSWindowCollectionBehavior behavior =
        NSWindowCollectionBehaviorCanJoinAllSpaces |
        NSWindowCollectionBehaviorStationary |
        NSWindowCollectionBehaviorIgnoresCycle |
        NSWindowCollectionBehaviorFullScreenAuxiliary;
    [window setCollectionBehavior:behavior];

    [window setLevel:NSFloatingWindowLevel];

    // 3. Clear background and opacity on contentView, superviews, and layers
    NSView *contentView = [window contentView];
    if (contentView) {
        contentView.wantsLayer = YES;
        contentView.layer.opaque = NO;
        contentView.layer.backgroundColor = [[NSColor clearColor] CGColor];

        NSView *superView = [contentView superview];
        while (superView != nil) {
            superView.wantsLayer = YES;
            superView.layer.opaque = NO;
            superView.layer.backgroundColor = [[NSColor clearColor] CGColor];
            superView = [superView superview];
        }

        MakeLayersTransparent([contentView layer]);

        NSTrackingArea *trackingArea = [[NSTrackingArea alloc] initWithRect:[contentView bounds]
            options:(NSTrackingMouseMoved | NSTrackingMouseEnteredAndExited | NSTrackingActiveAlways | NSTrackingInVisibleRect)
            owner:contentView
            userInfo:nil];
        [contentView addTrackingArea:trackingArea];
    }
}

static void DarwinDockToRightEdge(int width, int height, int state) {
    dispatch_async(dispatch_get_main_queue(), ^{
        SetupDarwinEditMenu();

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

static int DarwinIsMouseInsideWindow(void) {
    if (!g_appWindow) return 0;
    NSPoint mouseLoc = [NSEvent mouseLocation];
    NSRect winFrame = [g_appWindow frame];
    // Inset slightly by -6 to provide a stable hit-test buffer
    NSRect hitFrame = NSInsetRect(winFrame, -6, -6);
    return NSPointInRect(mouseLoc, hitFrame) ? 1 : 0;
}

static NSString *const kHideJiraHeaderScript =
    @"var css = 'header, #ak-jira-navigation, nav[aria-label=\"Global\"], "
    @"[data-testid=\"GlobalNavigation\"], [data-test-id=\"global-pages.header\"], "
    @"[data-testid=\"navigation-apps-sidebar\"], [data-testid=\"app-navigation\"], "
    @"[data-testid=\"NavigationHeader\"] { display: none !important; } "
    @"body, #ak-main-content, #jira-frontend, #content { margin-top: 0 !important; padding-top: 0 !important; }'; "
    @"var style = document.createElement('style'); "
    @"style.type = 'text/css'; "
    @"style.appendChild(document.createTextNode(css)); "
    @"(document.head || document.documentElement).appendChild(style);";

static void DarwinSetMobileWebViewVisible(int visible, int w, int h) {
    dispatch_async(dispatch_get_main_queue(), ^{
        SetupDarwinEditMenu();

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
                    WKUserContentController *userContent = [[WKUserContentController alloc] init];
                    WKUserScript *script = [[WKUserScript alloc] initWithSource:kHideJiraHeaderScript
                                                                  injectionTime:WKUserScriptInjectionTimeAtDocumentEnd
                                                               forMainFrameOnly:NO];
                    [userContent addUserScript:script];
                    config.userContentController = userContent;

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
                [g_ticketWebView evaluateJavaScript:kHideJiraHeaderScript completionHandler:nil];
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

func IsMouseInside() bool {
	return C.DarwinIsMouseInsideWindow() != 0
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
