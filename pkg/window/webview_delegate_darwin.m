//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

// Hosts of tickets loaded into the panel. Links the user clicks to any other host open
// in the default browser instead of inside the always-on-top panel. Redirects and
// script navigations (SSO login flows) are not link clicks and stay in the panel.
static NSMutableSet<NSString *> *g_allowedHosts = nil;

static BOOL JQAIsAllowedInPanel(NSURL *url) {
    NSString *scheme = [[url scheme] lowercaseString];
    if ([scheme isEqualToString:@"about"] || [scheme isEqualToString:@"blob"] || [scheme isEqualToString:@"data"]) {
        return YES;
    }
    NSString *host = [[url host] lowercaseString];
    return host != nil && [g_allowedHosts containsObject:host];
}

static void JQAOpenExternally(NSURL *url) {
    NSString *scheme = [[url scheme] lowercaseString];
    if ([scheme isEqualToString:@"http"] || [scheme isEqualToString:@"https"] || [scheme isEqualToString:@"mailto"]) {
        [[NSWorkspace sharedWorkspace] openURL:url];
    }
}

@interface JQAWebViewDelegate : NSObject <WKNavigationDelegate, WKUIDelegate>
@end

@implementation JQAWebViewDelegate
- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)action
    decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
    BOOL mainFrame = action.targetFrame == nil || action.targetFrame.isMainFrame;
    if (mainFrame && action.navigationType == WKNavigationTypeLinkActivated && !JQAIsAllowedInPanel(action.request.URL)) {
        JQAOpenExternally(action.request.URL);
        decisionHandler(WKNavigationActionPolicyCancel);
        return;
    }
    decisionHandler(WKNavigationActionPolicyAllow);
}

// target="_blank" links and window.open: stay in the panel for the ticket's own site,
// otherwise hand off to the browser. Returning nil never creates a second web view.
- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
    forNavigationAction:(WKNavigationAction *)action windowFeatures:(WKWindowFeatures *)windowFeatures {
    NSURL *url = action.request.URL;
    if (url != nil) {
        if (JQAIsAllowedInPanel(url)) {
            [webView loadRequest:action.request];
        } else {
            JQAOpenExternally(url);
        }
    }
    return nil;
}
@end

static JQAWebViewDelegate *g_webViewDelegate = nil; // WKWebView holds its delegates weakly

void JQAInstallWebViewDelegate(WKWebView *webView) {
    if (!g_webViewDelegate) g_webViewDelegate = [[JQAWebViewDelegate alloc] init];
    webView.navigationDelegate = g_webViewDelegate;
    webView.UIDelegate = g_webViewDelegate;
}

void JQAAllowHostInPanel(NSString *host) {
    if (host == nil) return;
    if (!g_allowedHosts) g_allowedHosts = [[NSMutableSet alloc] init];
    [g_allowedHosts addObject:[host lowercaseString]];
}
