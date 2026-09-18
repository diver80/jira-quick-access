//go:build darwin && cgo

package window

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework QuartzCore
#include <Cocoa/Cocoa.h>
#include <WebKit/WebKit.h>
#include <QuartzCore/QuartzCore.h>
#include <dispatch/dispatch.h>
#include <stdlib.h>
#include <string.h>

extern void goCollapseCallback(void);
extern void goDockChangedCallback(int side, int screenIndex, double yRatio);

static NSWindow *g_appWindow = nil;
static WKWebView *g_ticketWebView = nil;
static NSButton *g_closeButton = nil;
static int g_dockSide = 0;          // 0: Right, 1: Left
static int g_screenIndex = 0;       // Monitor index (0..N-1)
static double g_dockYRatio = 0.5;   // 0.0 - 1.0 (0.5 = centered)
static int g_isDragging = 0;
static int g_alwaysOnTop = 1;       // 1: Always on Top (NSStatusWindowLevel), 0: Normal
static int g_autoHide = 0;          // 1: Auto-Hide like macOS Dock, 0: Disabled
static int g_isTucked = 0;          // 1: Window is tucked into screen edge

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

// Allow borderless HUD window to become key, receive text input, and handle Escape / Cmd shortcuts
@interface NSWindow (AllowKeyWindow)
- (void)onCloseHUDClicked:(id)sender;
@end

@implementation NSWindow (AllowKeyWindow)
- (BOOL)canBecomeKeyWindow {
    return YES;
}
- (BOOL)canBecomeMainWindow {
    return YES;
}
- (void)onCloseHUDClicked:(id)sender {
    goCollapseCallback();
}
- (BOOL)performKeyEquivalent:(NSEvent *)event {
    // Escape key (keyCode 53) collapses view
    if ([event keyCode] == 53) {
        goCollapseCallback();
        return YES;
    }

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
        } else if ([chars isEqualToString:@"w"]) {
            goCollapseCallback();
            return YES;
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

    [window setStyleMask:NSWindowStyleMaskBorderless];
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
    [window setHidesOnDeactivate:NO];

    if (g_alwaysOnTop) {
        [window setLevel:NSStatusWindowLevel];
    } else {
        [window setLevel:NSNormalWindowLevel];
    }

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

static int DarwinGetScreensCount(void) {
    return (int)[[NSScreen screens] count];
}

static void DarwinGetScreenInfo(int idx, char *nameOut, int maxLen, int *isMainOut, int *xOut, int *yOut, int *wOut, int *hOut) {
    NSArray *screens = [NSScreen screens];
    if (idx < 0 || idx >= [screens count]) return;
    NSScreen *s = [screens objectAtIndex:idx];
    NSString *name = nil;
    if (@available(macOS 10.15, *)) {
        name = [s localizedName];
    }
    if (!name || [name length] == 0) {
        if (s == [NSScreen mainScreen]) {
            name = @"Main Display";
        } else {
            name = [NSString stringWithFormat:@"Display %d", idx + 1];
        }
    }
    const char *utf8 = [name UTF8String];
    if (utf8 && nameOut && maxLen > 0) {
        strncpy(nameOut, utf8, maxLen - 1);
        nameOut[maxLen - 1] = '\0';
    }
    if (isMainOut) *isMainOut = (s == [NSScreen mainScreen]) ? 1 : 0;
    NSRect f = [s frame];
    if (xOut) *xOut = (int)f.origin.x;
    if (yOut) *yOut = (int)f.origin.y;
    if (wOut) *wOut = (int)f.size.width;
    if (hOut) *hOut = (int)f.size.height;
}

static void DarwinDock(int width, int height, int animate) {
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

        NSArray *screens = [NSScreen screens];
        NSScreen *screen = nil;
        if (g_screenIndex >= 0 && g_screenIndex < [screens count]) {
            screen = [screens objectAtIndex:g_screenIndex];
        }
        if (!screen) {
            screen = [g_appWindow screen] ?: [NSScreen mainScreen];
        }
        if (!screen && [screens count] > 0) {
            screen = [screens objectAtIndex:0];
        }

        NSRect screenFrame = NSMakeRect(0, 0, 1440, 900);
        if (screen) {
            screenFrame = [screen visibleFrame];
            if (screenFrame.size.width <= 0 || screenFrame.size.height <= 0) {
                screenFrame = [screen frame];
            }
        }

        CGFloat x = 0;
        if (g_dockSide == 1) {
            // Docked to LEFT edge
            if (g_autoHide && g_isTucked && width <= 45) {
                x = screenFrame.origin.x - (CGFloat)width + 6.0;
            } else {
                x = screenFrame.origin.x;
            }
        } else {
            // Docked to RIGHT edge
            if (g_autoHide && g_isTucked && width <= 45) {
                x = screenFrame.origin.x + screenFrame.size.width - 6.0;
            } else {
                x = screenFrame.origin.x + screenFrame.size.width - (CGFloat)width;
            }
        }

        CGFloat availH = screenFrame.size.height - (CGFloat)height;
        if (availH < 0) availH = 0;
        CGFloat yRatio = (CGFloat)g_dockYRatio;
        if (yRatio < 0.0) yRatio = 0.0;
        if (yRatio > 1.0) yRatio = 1.0;
        CGFloat y = screenFrame.origin.y + availH * yRatio;

        NSRect frame = NSMakeRect(x, y, (CGFloat)width, (CGFloat)height);
        if (animate) {
            [NSAnimationContext runAnimationGroup:^(NSAnimationContext *context) {
                [context setDuration:0.2];
                [[g_appWindow animator] setFrame:frame display:YES];
            }];
        } else {
            [g_appWindow setFrame:frame display:YES animate:NO];
        }

        if (g_alwaysOnTop) {
            [g_appWindow setLevel:NSStatusWindowLevel];
            [g_appWindow setHidesOnDeactivate:NO];
            [g_appWindow orderFrontRegardless];
        } else {
            [g_appWindow setLevel:NSNormalWindowLevel];
            [g_appWindow makeKeyAndOrderFront:nil];
        }
    });
}

static void DarwinSetDockPosition(int side, int screenIndex, double yRatio, int width, int height) {
    g_dockSide = (side == 1) ? 1 : 0;
    if (screenIndex >= 0) g_screenIndex = screenIndex;
    if (yRatio >= 0.0 && yRatio <= 1.0) g_dockYRatio = yRatio;
    DarwinDock(width, height, 0);
}

static void DarwinSetAlwaysOnTop(int alwaysOnTop) {
    g_alwaysOnTop = alwaysOnTop ? 1 : 0;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_appWindow) return;
        if (g_alwaysOnTop) {
            [g_appWindow setLevel:NSStatusWindowLevel];
            [g_appWindow setHidesOnDeactivate:NO];
            [g_appWindow orderFrontRegardless];
        } else {
            [g_appWindow setLevel:NSNormalWindowLevel];
        }
    });
}

static void DarwinSetAutoHide(int autoHide) {
    g_autoHide = autoHide ? 1 : 0;
    if (!g_autoHide && g_isTucked) {
        g_isTucked = 0;
        dispatch_async(dispatch_get_main_queue(), ^{
            if (g_appWindow) {
                DarwinDock((int)[g_appWindow frame].size.width, (int)[g_appWindow frame].size.height, 1);
            }
        });
    }
}

static void DarwinSetTucked(int tucked, int width, int height) {
    int shouldTuck = tucked ? 1 : 0;
    if (g_isTucked == shouldTuck) return;
    g_isTucked = shouldTuck;
    dispatch_async(dispatch_get_main_queue(), ^{
        DarwinDock(width, height, 1);
    });
}

static int DarwinGetAlwaysOnTop(void) {
    return g_alwaysOnTop;
}

static int DarwinGetAutoHide(void) {
    return g_autoHide;
}

static void DarwinStartWindowDrag(int width, int height) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_appWindow) return;
        if (g_isDragging) return;
        g_isDragging = 1;

        NSPoint startMouse = [NSEvent mouseLocation];
        NSRect startFrame = [g_appWindow frame];

        while (1) {
            NSEvent *event = [NSApp nextEventMatchingMask:(NSEventMaskLeftMouseDragged | NSEventMaskLeftMouseUp)
                                                untilDate:[NSDate distantFuture]
                                                   inMode:NSEventTrackingRunLoopMode
                                                  dequeue:YES];
            if (!event) break;

            if ([event type] == NSEventTypeLeftMouseUp) {
                break;
            }

            if ([event type] == NSEventTypeLeftMouseDragged) {
                NSPoint currentMouse = [NSEvent mouseLocation];
                CGFloat dx = currentMouse.x - startMouse.x;
                CGFloat dy = currentMouse.y - startMouse.y;

                NSRect newFrame = NSMakeRect(startFrame.origin.x + dx,
                                             startFrame.origin.y + dy,
                                             startFrame.size.width,
                                             startFrame.size.height);
                [g_appWindow setFrame:newFrame display:YES animate:NO];
            }
        }

        g_isDragging = 0;

        // Snap window to the nearest edge of whichever screen the mouse dropped on
        NSPoint dropMouse = [NSEvent mouseLocation];
        NSArray *screens = [NSScreen screens];
        NSScreen *targetScreen = nil;
        int targetScreenIdx = 0;

        for (int i = 0; i < [screens count]; i++) {
            NSScreen *s = [screens objectAtIndex:i];
            if (NSPointInRect(dropMouse, [s frame])) {
                targetScreen = s;
                targetScreenIdx = i;
                break;
            }
        }
        if (!targetScreen) {
            targetScreen = [g_appWindow screen] ?: [NSScreen mainScreen];
            targetScreenIdx = (int)[screens indexOfObject:targetScreen];
            if (targetScreenIdx < 0 || targetScreenIdx >= [screens count]) {
                targetScreenIdx = 0;
            }
        }

        NSRect visibleFrame = [targetScreen visibleFrame];
        if (visibleFrame.size.width <= 0 || visibleFrame.size.height <= 0) {
            visibleFrame = [targetScreen frame];
        }

        // Determine Left or Right edge
        CGFloat leftDist = fabs(dropMouse.x - visibleFrame.origin.x);
        CGFloat rightDist = fabs((visibleFrame.origin.x + visibleFrame.size.width) - dropMouse.x);

        int newDockSide = (leftDist < rightDist) ? 1 : 0; // 0: Right, 1: Left

        // Determine Y position ratio
        CGFloat curY = [g_appWindow frame].origin.y;
        CGFloat availH = visibleFrame.size.height - (CGFloat)height;
        if (availH < 1) availH = 1;
        CGFloat yRatio = (curY - visibleFrame.origin.y) / availH;
        if (yRatio < 0.0) yRatio = 0.0;
        if (yRatio > 1.0) yRatio = 1.0;

        g_dockSide = newDockSide;
        g_screenIndex = targetScreenIdx;
        g_dockYRatio = (double)yRatio;

        CGFloat targetX = (newDockSide == 0)
            ? (visibleFrame.origin.x + visibleFrame.size.width - (CGFloat)width)
            : visibleFrame.origin.x;
        CGFloat targetY = visibleFrame.origin.y + availH * yRatio;

        NSRect snapFrame = NSMakeRect(targetX, targetY, (CGFloat)width, (CGFloat)height);
        [g_appWindow setFrame:snapFrame display:YES animate:YES];

        goDockChangedCallback(newDockSide, targetScreenIdx, (double)yRatio);
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
            CGFloat cardW = (CGFloat)(w - 118);
            CGFloat cardH = (CGFloat)(h - 16);
            CGFloat cardX = (g_dockSide == 1) ? 110.0 : 8.0;
            CGFloat closeX = (g_dockSide == 1) ? 118.0 : 16.0;

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

                    g_ticketWebView = [[WKWebView alloc] initWithFrame:NSMakeRect(cardX, 8, cardW, cardH) configuration:config];
                    [g_ticketWebView setWantsLayer:YES];
                    [g_ticketWebView.layer setCornerRadius:14.0];
                    [g_ticketWebView.layer setMasksToBounds:YES];
                    [g_ticketWebView.layer setBorderWidth:1.5];
                    [g_ticketWebView.layer setBorderColor:[[NSColor colorWithCalibratedWhite:1.0 alpha:0.40] CGColor]];
                    [g_ticketWebView.layer setBackgroundColor:[[NSColor colorWithCalibratedRed:0.07 green:0.09 blue:0.15 alpha:0.98] CGColor]];
                    [g_ticketWebView.layer setZPosition:0.0];
                    [contentView addSubview:g_ticketWebView];

                    // Floating sleek high-contrast Close button (✕)
                    g_closeButton = [[NSButton alloc] initWithFrame:NSMakeRect(closeX, (CGFloat)(h - 44), 32, 32)];
                    [g_closeButton setBezelStyle:NSBezelStyleRegularSquare];
                    [g_closeButton setButtonType:NSButtonTypeMomentaryPushIn];
                    [g_closeButton setBordered:NO];
                    [g_closeButton setTarget:g_appWindow];
                    [g_closeButton setAction:@selector(onCloseHUDClicked:)];
                    [g_closeButton setWantsLayer:YES];
                    [g_closeButton.layer setCornerRadius:16.0];
                    [g_closeButton.layer setMasksToBounds:YES];
                    [g_closeButton.layer setBorderWidth:1.5];
                    [g_closeButton.layer setBorderColor:[[NSColor colorWithCalibratedWhite:1.0 alpha:0.65] CGColor]];
                    [g_closeButton.layer setBackgroundColor:[[NSColor colorWithCalibratedRed:0.08 green:0.11 blue:0.18 alpha:0.96] CGColor]];
                    [g_closeButton.layer setZPosition:9999.0];

                    NSDictionary *attrs = @{
                        NSForegroundColorAttributeName: [NSColor whiteColor],
                        NSFontAttributeName: [NSFont boldSystemFontOfSize:14.0]
                    };
                    NSAttributedString *attrTitle = [[NSAttributedString alloc] initWithString:@"✕" attributes:attrs];
                    [g_closeButton setAttributedTitle:attrTitle];

                    [contentView addSubview:g_closeButton positioned:NSWindowAbove relativeTo:nil];
                }
            }
            if (g_ticketWebView) {
                [g_ticketWebView setFrame:NSMakeRect(cardX, 8, cardW, cardH)];
                [g_ticketWebView.layer setCornerRadius:14.0];
                [g_ticketWebView.layer setMasksToBounds:YES];
                [g_ticketWebView.layer setBorderWidth:1.5];
                [g_ticketWebView.layer setBorderColor:[[NSColor colorWithCalibratedWhite:1.0 alpha:0.40] CGColor]];
                [g_ticketWebView.layer setZPosition:0.0];
                [g_ticketWebView setHidden:NO];
                [g_ticketWebView evaluateJavaScript:kHideJiraHeaderScript completionHandler:nil];
            }
            if (g_closeButton) {
                [g_closeButton setFrame:NSMakeRect(closeX, (CGFloat)(h - 44), 32, 32)];
                [g_closeButton.layer setZPosition:9999.0];
                NSView *cv = [g_appWindow contentView];
                if (cv) {
                    [cv addSubview:g_closeButton positioned:NSWindowAbove relativeTo:nil];
                }
                [g_closeButton setHidden:NO];
            }
            if (g_appWindow) {
                [g_appWindow makeKeyAndOrderFront:nil];
            }
        } else {
            if (g_ticketWebView) {
                [g_ticketWebView setHidden:YES];
            }
            if (g_closeButton) {
                [g_closeButton setHidden:YES];
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

static void DarwinCopyText(const char *text) {
    if (!text) return;
    NSString *nsText = [NSString stringWithUTF8String:text];
    dispatch_async(dispatch_get_main_queue(), ^{
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        [pb setString:nsText forType:NSPasteboardTypeString];
    });
}

static char *DarwinPasteText(void) {
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    NSString *val = [pb stringForType:NSPasteboardTypeString];
    if (!val) return NULL;
    const char *utf8 = [val UTF8String];
    return utf8 ? strdup(utf8) : NULL;
}

static void DarwinSetToolTip(const char *text) {
    NSString *tip = (text && strlen(text) > 0) ? [NSString stringWithUTF8String:text] : nil;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_appWindow) return;
        NSView *cv = [g_appWindow contentView];
        if (!cv) return;
        [cv removeAllToolTips];
        if (tip) {
            [cv setToolTip:tip];
        }
    });
}
*/
import "C"
import (
	"sync"
	"unsafe"
)

var (
	collapseMu         sync.Mutex
	collapseCallback   func()
	dockChangeMu       sync.Mutex
	dockChangeCallback DockChangeCallback
)

//export goCollapseCallback
func goCollapseCallback() {
	collapseMu.Lock()
	cb := collapseCallback
	collapseMu.Unlock()
	if cb != nil {
		cb()
	}
}

//export goDockChangedCallback
func goDockChangedCallback(side C.int, screenIndex C.int, yRatio C.double) {
	dockChangeMu.Lock()
	cb := dockChangeCallback
	dockChangeMu.Unlock()
	if cb != nil {
		cb(DockSide(side), int(screenIndex), float64(yRatio))
	}
}

func RegisterCollapseHandler(cb func()) {
	collapseMu.Lock()
	collapseCallback = cb
	collapseMu.Unlock()
}

func RegisterDockChangeHandler(cb DockChangeCallback) {
	dockChangeMu.Lock()
	dockChangeCallback = cb
	dockChangeMu.Unlock()
}

type DarwinManager struct {
	mu            sync.RWMutex
	state         WindowState
	dockSide      DockSide
	monitorIndex  int
	posYRatio     float64
	currentWidth  int
	currentHeight int
}

func init() {
	if DefaultManager == nil {
		DefaultManager = &DarwinManager{
			state:         StateRest,
			dockSide:      DockSideRight,
			monitorIndex:  0,
			posYRatio:     0.5,
			currentWidth:  36,
			currentHeight: 224,
		}
	}
}

func (m *DarwinManager) InitEdgeRail(width, height int) error {
	m.mu.Lock()
	m.currentWidth = width
	m.currentHeight = height
	m.mu.Unlock()
	m.SetState(StateRest, width, height)
	return nil
}

func (m *DarwinManager) SetState(state WindowState, width, height int) {
	m.mu.Lock()
	m.state = state
	m.currentWidth = width
	m.currentHeight = height
	st := m.state
	m.mu.Unlock()
	C.DarwinDock(C.int(width), C.int(height), C.int(st))
}

func (m *DarwinManager) Dock(width, height int) {
	m.mu.Lock()
	m.currentWidth = width
	m.currentHeight = height
	st := m.state
	m.mu.Unlock()
	C.DarwinDock(C.int(width), C.int(height), C.int(st))
}

func (m *DarwinManager) DockToRightEdge(width, height int) {
	m.SetDockSide(DockSideRight)
	m.Dock(width, height)
}

func (m *DarwinManager) SetDockSide(side DockSide) {
	m.mu.Lock()
	m.dockSide = side
	w := m.currentWidth
	h := m.currentHeight
	if w <= 0 {
		w = 36
	}
	if h <= 0 {
		h = 224
	}
	idx := m.monitorIndex
	ratio := m.posYRatio
	m.mu.Unlock()

	C.DarwinSetDockPosition(C.int(side), C.int(idx), C.double(ratio), C.int(w), C.int(h))
}

func (m *DarwinManager) GetDockSide() DockSide {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dockSide
}

func (m *DarwinManager) SetMonitor(screenIndex int) {
	m.mu.Lock()
	m.monitorIndex = screenIndex
	side := m.dockSide
	ratio := m.posYRatio
	w := m.currentWidth
	h := m.currentHeight
	if w <= 0 {
		w = 36
	}
	if h <= 0 {
		h = 224
	}
	m.mu.Unlock()

	C.DarwinSetDockPosition(C.int(side), C.int(screenIndex), C.double(ratio), C.int(w), C.int(h))
}

func (m *DarwinManager) GetSelectedMonitor() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.monitorIndex
}

func (m *DarwinManager) GetMonitors() []MonitorInfo {
	count := int(C.DarwinGetScreensCount())
	if count <= 0 {
		return []MonitorInfo{
			{Index: 0, Name: "Main Display", IsMain: true, Width: 1920, Height: 1080},
		}
	}
	var res []MonitorInfo
	nameBuf := make([]byte, 256)
	for i := 0; i < count; i++ {
		var isMain, x, y, w, h C.int
		C.DarwinGetScreenInfo(
			C.int(i),
			(*C.char)(unsafe.Pointer(&nameBuf[0])),
			C.int(len(nameBuf)),
			&isMain,
			&x,
			&y,
			&w,
			&h,
		)
		name := C.GoString((*C.char)(unsafe.Pointer(&nameBuf[0])))
		if name == "" {
			name = "Display"
		}
		res = append(res, MonitorInfo{
			Index:  i,
			Name:   name,
			IsMain: isMain != 0,
			X:      int(x),
			Y:      int(y),
			Width:  int(w),
			Height: int(h),
		})
	}
	return res
}

func (m *DarwinManager) SetPositionRatio(ratio float64) {
	if ratio < 0.0 {
		ratio = 0.0
	}
	if ratio > 1.0 {
		ratio = 1.0
	}
	m.mu.Lock()
	m.posYRatio = ratio
	side := m.dockSide
	idx := m.monitorIndex
	w := m.currentWidth
	h := m.currentHeight
	if w <= 0 {
		w = 36
	}
	if h <= 0 {
		h = 224
	}
	m.mu.Unlock()

	C.DarwinSetDockPosition(C.int(side), C.int(idx), C.double(ratio), C.int(w), C.int(h))
}

func (m *DarwinManager) GetPositionRatio() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.posYRatio
}

func (m *DarwinManager) StartWindowDrag() {
	m.mu.RLock()
	w := m.currentWidth
	h := m.currentHeight
	if w <= 0 {
		w = 36
	}
	if h <= 0 {
		h = 224
	}
	m.mu.RUnlock()
	C.DarwinStartWindowDrag(C.int(w), C.int(h))
}

func (m *DarwinManager) OpenTicketURL(url string) error {
	return OpenURL(url)
}

func (m *DarwinManager) SetAlwaysOnTop(alwaysOnTop bool) {
	val := 0
	if alwaysOnTop {
		val = 1
	}
	C.DarwinSetAlwaysOnTop(C.int(val))
}

func (m *DarwinManager) GetAlwaysOnTop() bool {
	return C.DarwinGetAlwaysOnTop() != 0
}

func (m *DarwinManager) SetAutoHide(autoHide bool) {
	val := 0
	if autoHide {
		val = 1
	}
	C.DarwinSetAutoHide(C.int(val))
}

func (m *DarwinManager) GetAutoHide() bool {
	return C.DarwinGetAutoHide() != 0
}

func (m *DarwinManager) SetTucked(tucked bool, width, height int) {
	val := 0
	if tucked {
		val = 1
	}
	if width <= 0 {
		width = 36
	}
	if height <= 0 {
		height = 224
	}
	C.DarwinSetTucked(C.int(val), C.int(width), C.int(height))
}

func (m *DarwinManager) SetToolTip(tooltip string) {
	cTip := C.CString(tooltip)
	defer C.free(unsafe.Pointer(cTip))
	C.DarwinSetToolTip(cTip)
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

func NativeCopyText(text string) {
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	C.DarwinCopyText(cText)
}

func NativePasteText() (string, bool) {
	cStr := C.DarwinPasteText()
	if cStr == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(cStr))
	return C.GoString(cStr), true
}
