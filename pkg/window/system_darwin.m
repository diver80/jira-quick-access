//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <Carbon/Carbon.h>
#import <ServiceManagement/ServiceManagement.h>
#import <UserNotifications/UserNotifications.h>

extern void goCollapseCallback(void);
extern void goHotKeyPressed(void);
extern void goNotificationClicked(char *issueKey, char *instanceID);
extern void JQAOpenPanelPageExternally(WKWebView *webView, NSURL *fallback);

// ---------------------------------------------------------------------------
// Global hotkey (⌥⌘J). Carbon hot keys need no Accessibility permission.
// ---------------------------------------------------------------------------

static EventHotKeyRef g_hotKeyRef = NULL;
static EventHandlerRef g_hotKeyHandler = NULL;

static OSStatus JQAHotKeyHandler(EventHandlerCallRef next, EventRef event, void *userData) {
    goHotKeyPressed();
    return noErr;
}

void JQASetGlobalHotKey(int enabled) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_hotKeyHandler) {
            EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
            InstallApplicationEventHandler(&JQAHotKeyHandler, 1, &spec, NULL, &g_hotKeyHandler);
        }
        if (enabled && !g_hotKeyRef) {
            EventHotKeyID hotKeyID = {'JQAk', 1};
            RegisterEventHotKey(kVK_ANSI_J, cmdKey | optionKey, hotKeyID, GetApplicationEventTarget(), 0, &g_hotKeyRef);
        } else if (!enabled && g_hotKeyRef) {
            UnregisterEventHotKey(g_hotKeyRef);
            g_hotKeyRef = NULL;
        }
    });
}

// Brings the HUD to the front with keyboard focus, so typing goes to the search field.
void JQAActivateApp(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp activateIgnoringOtherApps:YES];
        NSArray *windows = [NSApp windows];
        if ([windows count] > 0) {
            [[windows objectAtIndex:0] makeKeyAndOrderFront:nil];
        }
    });
}

// ---------------------------------------------------------------------------
// Launch at login (SMAppService, macOS 13+)
// ---------------------------------------------------------------------------

// Returns 1 enabled, 0 disabled, 2 waiting for approval in System Settings,
// -1 unsupported (macOS < 13 or not running from an app bundle).
int JQALoginItemStatus(void) {
    if (@available(macOS 13.0, *)) {
        if ([[NSBundle mainBundle] bundleIdentifier] == nil) return -1;
        switch ([[SMAppService mainAppService] status]) {
            case SMAppServiceStatusEnabled: return 1;
            case SMAppServiceStatusRequiresApproval: return 2;
            case SMAppServiceStatusNotFound: return -1;
            default: return 0;
        }
    }
    return -1;
}

// Returns NULL on success, otherwise a malloc'd error message the caller frees.
char *JQASetLoginItem(int enabled) {
    if (@available(macOS 13.0, *)) {
        if ([[NSBundle mainBundle] bundleIdentifier] == nil) return strdup("not running from an app bundle");
        NSError *error = nil;
        BOOL ok = enabled ? [[SMAppService mainAppService] registerAndReturnError:&error]
                          : [[SMAppService mainAppService] unregisterAndReturnError:&error];
        if (ok) return NULL;
        const char *msg = [[error localizedDescription] UTF8String];
        return strdup(msg ? msg : "unknown error");
    }
    return strdup("requires macOS 13 or newer");
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

@interface JQANotificationDelegate : NSObject <UNUserNotificationCenterDelegate>
@end

@implementation JQANotificationDelegate
// The HUD counts as the active app, so banners must be requested explicitly.
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completionHandler {
    completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList | UNNotificationPresentationOptionSound);
}

- (void)userNotificationCenter:(UNUserNotificationCenter *)center
didReceiveNotificationResponse:(UNNotificationResponse *)response
         withCompletionHandler:(void (^)(void))completionHandler {
    NSDictionary *info = response.notification.request.content.userInfo;
    NSString *key = info[@"key"];
    NSString *inst = info[@"instance"];
    if ([key length] > 0) {
        goNotificationClicked((char *)[key UTF8String], (char *)[(inst ?: @"") UTF8String]);
    }
    completionHandler();
}
@end

static JQANotificationDelegate *g_notificationDelegate = nil;

// UNUserNotificationCenter aborts the process outside an app bundle, so every
// entry point checks for one first.
static BOOL JQAHasBundle(void) {
    return [[NSBundle mainBundle] bundleIdentifier] != nil;
}

int JQANotificationsInit(void) {
    if (!JQAHasBundle()) return 0;
    dispatch_async(dispatch_get_main_queue(), ^{
        UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
        if (!g_notificationDelegate) {
            g_notificationDelegate = [[JQANotificationDelegate alloc] init];
            center.delegate = g_notificationDelegate;
        }
        [center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionSound)
                              completionHandler:^(BOOL granted, NSError *error) {}];
    });
    return 1;
}

void JQANotify(const char *title, const char *body, const char *issueKey, const char *instanceID) {
    if (!JQAHasBundle()) return;
    UNMutableNotificationContent *content = [[UNMutableNotificationContent alloc] init];
    content.title = [NSString stringWithUTF8String:title];
    content.body = [NSString stringWithUTF8String:body];
    content.sound = [UNNotificationSound defaultSound];
    content.userInfo = @{
        @"key": [NSString stringWithUTF8String:issueKey],
        @"instance": [NSString stringWithUTF8String:instanceID],
    };
    UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:[[NSUUID UUID] UUIDString]
                                                                          content:content
                                                                          trigger:nil];
    [content release];
    [[UNUserNotificationCenter currentNotificationCenter] addNotificationRequest:request withCompletionHandler:nil];
}

// ---------------------------------------------------------------------------
// Ticket panel header: key, summary and actions above the web view
// ---------------------------------------------------------------------------

static const CGFloat kHeaderHeight = 36.0;

@interface JQAHeaderView : NSView
@property (nonatomic, assign) WKWebView *webView; // owned by manager_darwin.go
@property (nonatomic, copy) NSString *ticketKey;
@property (nonatomic, copy) NSString *branchName;
@property (nonatomic, retain) NSURL *ticketURL;
@property (nonatomic, retain) NSTextField *keyLabel;
@property (nonatomic, retain) NSTextField *summaryLabel;
@property (nonatomic, retain) NSButton *closeButton;
@property (nonatomic, retain) NSButton *keyButton;
@property (nonatomic, retain) NSButton *linkButton;
@property (nonatomic, retain) NSButton *branchButton;
@property (nonatomic, retain) NSButton *browserButton;
@end

@implementation JQAHeaderView

- (BOOL)isFlipped {
    return YES;
}

static NSTextField *JQAHeaderLabel(CGFloat size, BOOL bold, NSColor *color) {
    NSTextField *label = [NSTextField labelWithString:@""];
    [label setFont:bold ? [NSFont boldSystemFontOfSize:size] : [NSFont systemFontOfSize:size]];
    [label setTextColor:color];
    [label setLineBreakMode:NSLineBreakByTruncatingTail];
    [[label cell] setUsesSingleLineMode:YES];
    return [label retain];
}

- (NSButton *)buttonWithTitle:(NSString *)title action:(SEL)action toolTip:(NSString *)toolTip {
    NSButton *button = [[NSButton alloc] initWithFrame:NSZeroRect];
    [button setBordered:NO];
    [button setButtonType:NSButtonTypeMomentaryPushIn];
    [button setTarget:self];
    [button setAction:action];
    [button setToolTip:toolTip];
    [button setWantsLayer:YES];
    [button.layer setCornerRadius:6.0];
    [button.layer setBorderWidth:1.0];
    [button.layer setBorderColor:[[NSColor colorWithCalibratedWhite:1.0 alpha:0.22] CGColor]];
    [button.layer setBackgroundColor:[[NSColor colorWithCalibratedRed:0.14 green:0.18 blue:0.26 alpha:1.0] CGColor]];
    [self setTitle:title ofButton:button];
    [self addSubview:button];
    return button;
}

- (void)setTitle:(NSString *)title ofButton:(NSButton *)button {
    NSDictionary *attrs = @{
        NSForegroundColorAttributeName: [NSColor colorWithCalibratedRed:0.89 green:0.93 blue:0.98 alpha:1.0],
        NSFontAttributeName: [NSFont systemFontOfSize:11.0 weight:NSFontWeightMedium]
    };
    NSAttributedString *attrTitle = [[NSAttributedString alloc] initWithString:title attributes:attrs];
    [button setAttributedTitle:attrTitle];
    [attrTitle release];
}

- (instancetype)initWithWebView:(WKWebView *)webView {
    self = [super initWithFrame:NSZeroRect];
    if (self) {
        self.webView = webView;
        [self setWantsLayer:YES];
        [self.layer setCornerRadius:10.0];
        [self.layer setBorderWidth:1.0];
        [self.layer setBorderColor:[[NSColor colorWithCalibratedWhite:1.0 alpha:0.30] CGColor]];
        [self.layer setBackgroundColor:[[NSColor colorWithCalibratedRed:0.08 green:0.11 blue:0.17 alpha:0.98] CGColor]];

        self.keyLabel = JQAHeaderLabel(12.0, YES, [NSColor whiteColor]);
        self.summaryLabel = JQAHeaderLabel(11.0, NO, [NSColor colorWithCalibratedRed:0.62 green:0.68 blue:0.78 alpha:1.0]);
        [self.keyLabel release];
        [self.summaryLabel release];
        [self addSubview:self.keyLabel];
        [self addSubview:self.summaryLabel];

        self.closeButton = [self buttonWithTitle:@"✕" action:@selector(onClose:) toolTip:@"Close (Esc)"];
        self.keyButton = [self buttonWithTitle:@"Copy key" action:@selector(onCopyKey:) toolTip:@"Copy the ticket key (⌘C)"];
        self.linkButton = [self buttonWithTitle:@"Copy link" action:@selector(onCopyLink:) toolTip:@"Copy the ticket link"];
        self.branchButton = [self buttonWithTitle:@"Copy branch" action:@selector(onCopyBranch:) toolTip:@"Copy a Git branch name (⌘⇧C)"];
        self.browserButton = [self buttonWithTitle:@"Open ↗" action:@selector(onOpenBrowser:) toolTip:@"Open in browser"];
        [self.closeButton release];
        [self.keyButton release];
        [self.linkButton release];
        [self.branchButton release];
        [self.browserButton release];
    }
    return self;
}

- (void)layout {
    [super layout];
    CGFloat w = self.bounds.size.width;
    CGFloat y = 6.0, h = kHeaderHeight - 12.0, gap = 6.0;

    [self.closeButton setFrame:NSMakeRect(8, y, 28, h)];

    CGFloat x = w - 8;
    NSArray *right = @[self.browserButton, self.branchButton, self.linkButton, self.keyButton];
    NSArray *widths = @[@68, @86, @70, @68];
    for (NSUInteger i = 0; i < [right count]; i++) {
        NSButton *button = right[i];
        if ([button isHidden]) continue;
        CGFloat bw = [widths[i] doubleValue];
        x -= bw;
        [button setFrame:NSMakeRect(x, y, bw, h)];
        x -= gap;
    }

    CGFloat textX = 46;
    CGFloat keyW = MIN(ceil([self.keyLabel intrinsicContentSize].width) + 4, 140);
    [self.keyLabel setFrame:NSMakeRect(textX, 9, keyW, 18)];
    CGFloat summaryX = textX + keyW + 6;
    [self.summaryLabel setFrame:NSMakeRect(summaryX, 10, MAX(0, x - summaryX - 4), 16)];
}

- (void)flash:(NSButton *)button title:(NSString *)title {
    [self setTitle:@"✓ Copied" ofButton:button];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.2 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
        [self setTitle:title ofButton:button];
    });
}

static void JQACopy(NSString *text) {
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    [pb setString:text forType:NSPasteboardTypeString];
}

- (void)onClose:(id)sender {
    goCollapseCallback();
}

- (void)onCopyKey:(id)sender {
    if ([self.ticketKey length] == 0) return;
    JQACopy(self.ticketKey);
    [self flash:sender title:@"Copy key"];
}

- (void)onCopyLink:(id)sender {
    if (!self.ticketURL) return;
    JQACopy([self.ticketURL absoluteString]);
    [self flash:sender title:@"Copy link"];
}

- (void)onCopyBranch:(id)sender {
    if ([self.branchName length] == 0) return;
    JQACopy(self.branchName);
    [self flash:sender title:@"Copy branch"];
}

- (void)onOpenBrowser:(id)sender {
    JQAOpenPanelPageExternally(self.webView, self.ticketURL);
}

@end

static JQAHeaderView *g_header = nil;

NSView *JQAHeaderCreate(NSView *contentView, WKWebView *webView) {
    if (!g_header) {
        g_header = [[JQAHeaderView alloc] initWithWebView:webView];
        [contentView addSubview:g_header positioned:NSWindowAbove relativeTo:nil];
    }
    return g_header;
}

CGFloat JQAHeaderHeight(void) {
    return kHeaderHeight;
}

void JQAHeaderSetTicket(const char *key, const char *summary, const char *url, const char *branch) {
    NSString *nsKey = [NSString stringWithUTF8String:key];
    NSString *nsSummary = [NSString stringWithUTF8String:summary];
    NSString *nsURL = [NSString stringWithUTF8String:url];
    NSString *nsBranch = [NSString stringWithUTF8String:branch];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_header) return;
        g_header.ticketKey = nsKey;
        g_header.branchName = nsBranch;
        g_header.ticketURL = [nsURL length] > 0 ? [NSURL URLWithString:nsURL] : nil;
        [g_header.keyLabel setStringValue:nsKey];
        [g_header.summaryLabel setStringValue:nsSummary];
        [g_header.summaryLabel setToolTip:nsSummary];
        // The offline demo card has no Jira page to link to.
        BOOL hasURL = g_header.ticketURL != nil;
        [g_header.linkButton setHidden:!hasURL];
        [g_header.browserButton setHidden:!hasURL];
        [g_header.branchButton setHidden:[nsBranch length] == 0];
        [g_header setNeedsLayout:YES];
    });
}
