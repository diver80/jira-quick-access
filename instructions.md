
+-----------------------------------------------------------------------------------+
|                                  STATE MACHINE                                    |
+-----------------------------------------------------------------------------------+
|                                                                                   |
|  +---------------------+        Mouse Entered (>150ms)        +----------------+  |
|  |                     | -----------------------------------> |                |  |
|  |   COLLAPSED RAIL    |                                      | EXPANDED PANEL |  |
|  |     (Width 36px)    | <----------------------------------- |  (Width 360px) |  |
|  +---------------------+        Mouse Exited (>250ms)         +----------------+  |
|         |        ^                   (Unpinned)                       |     ^     |
|         |        |                                                    |     |     |
|         | Click  | Close                                        Click |     |     |
|         | Gear   | Settings                                    Config |     |     |
|         v        |                                                    v     |     |
|  +-----------------------------------------------------------------------------+  |
|  |                              SETTINGS OVERLAY                               |  |
|  |        (Jira Cloud URL, Auth Token, Custom JQL, Polling Interval)           |  |
|  +-----------------------------------------------------------------------------+  |
|                                                                                   |
+-----------------------------------------------------------------------------------+

### 2.1 Visual ASCII Layouts

#### Collapsed State (36 px)

+----+
| ⚙️ |  <-- Settings modal trigger
+----+
| 🟢 |  <-- PROJ-102 (In Progress)
|    |
| 🟡 |  <-- PROJ-98  (In Review)
|    |
| ⚪ |  <-- PROJ-115 (To Do)
|    |
| 🔍 |  <-- Quick Search trigger
+----+

#### Expanded State (360 px)

+-------------------------------------------------------------+
| ⚙️ Jira Quick Access                        🔄 📌 [X]        |
+-------------------------------------------------------------+
| 🔍 Filter or Search Issue... (Cmd+K)                        |
+-------------------------------------------------------------+
| 📌 PINNED & IN PROGRESS                                     |
|  🟢 PROJ-102 Implement OAuth PKCE Flow                      |
|     Status: [ In Progress ▾ ]    [📋 Branch]  [↗ Browser]   |
+-------------------------------------------------------------+
| 📥 ASSIGNED TO ME (3)                                       |
|  🟡 PROJ-98 Fix race condition in token poller              |
|     Status: [ In Review ▾ ]      [📋 Key]     [↗ Browser]   |
|                                                             |
|  ⚪ PROJ-115 Update deployment manifest                     |
|     Status: [ To Do ▾ ]          [📋 Key]     [↗ Browser]   |
+-------------------------------------------------------------+
| ⚡ RECENT TRANSITIONS                                        |
|  ✓ PROJ-90 moved to Done (10m ago)                          |
+-------------------------------------------------------------+

---

## 3. macOS Native Windowing & Cgo Integration

To achieve a true borderless floating edge drawer on macOS without standard title bars or window frames:

### 3.1 Objective-C Window Configuration (`bridge_darwin.m`)
```objc
#import <Cocoa/Cocoa.h>

@interface EdgePanel : NSPanel
@end

@implementation EdgePanel
- (BOOL)canBecomeKeyWindow {
    return YES; // Allow keyboard inputs for search & config fields
}
- (BOOL)canBecomeMainWindow {
    return NO;
}
@end

void* CreateEdgeWindow(int x, int y, int width, int height) {
    NSRect contentRect = NSMakeRect(x, y, width, height);
    NSUInteger styleMask = NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel;

    EdgePanel* window = [[EdgePanel alloc] initWithContentRect:contentRect
                                                     styleMask:styleMask
                                                       backing:NSBackingStoreBuffered
                                                         defer:NO];

    [window setLevel:NSFloatingWindowLevel]; // Float above normal app windows
    [window setOpaque:NO];
    [window setBackgroundColor:[NSColor colorWithCalibratedWhite:0.1 alpha:0.95]];
    [window setHasShadow:YES];
    
    // Multi-desktop persistence
    [window setCollectionBehavior:(NSWindowCollectionBehaviorCanJoinAllSpaces |
                                   NSWindowCollectionBehaviorStationary |
                                   NSWindowCollectionBehaviorFullScreenAuxiliary)];

    // Mouse tracking for hover expand/collapse
    NSTrackingAreaOptions options = NSTrackingMouseEnteredAndExited | 
                                    NSTrackingActiveAlways | 
                                    NSTrackingInVisibleRect;
    NSTrackingArea* trackingArea = [[NSTrackingArea alloc] initWithRect:[[window contentView] bounds]
                                                                options:options
                                                                  owner:[window contentView]
                                                               userInfo:nil];
    [[window contentView] addTrackingArea:trackingArea];

    [window makeKeyAndOrderFront:nil];
    return (__bridge void*)window;
}

void SetWindowFrame(void* windowPtr, int x, int y, int width, int height, BOOL animate) {
    NSWindow* window = (__bridge NSWindow*)windowPtr;
    NSRect newRect = NSMakeRect(x, y, width, height);
    [window setFrame:newRect display:YES animate:animate];
}

4. Jira REST API v3 Integration
4.1 Required Endpoints
Purpose	Method	Endpoint	Payload / Params
Fetch Assigned Issues	GET	/rest/api/3/search	jql=assignee=currentUser()+AND+resolution=Unresolved+ORDER+BY+updated+DESC&fields=summary,status,priority,issuetype,updated
Get Available Transitions	GET	/rest/api/3/issue/{issueIdOrKey}/transitions	-
Execute Transition	POST	/rest/api/3/issue/{issueIdOrKey}/transitions	{"transition": {"id": "<transition_id>"}}
Issue Picker / Quick Search	GET	/rest/api/3/issue/picker	query={term}&currentJQL=assignee=currentUser()
User Profile / Verification	GET	/rest/api/3/myself	Verify auth token and account validity
4.2 Data Models (Go Structs)
package jira

import "time"

type Config struct {
	BaseURL       string `json:"base_url"`       // e.g. "[https://company.atlassian.net](https://company.atlassian.net)"
	Email         string `json:"email"`          // User email for basic auth with API token
	JQLQuery      string `json:"jql_query"`      // Default fallback query
	PollInterval  int    `json:"poll_interval"`  // In seconds (e.g., 60)
	BranchPrefix  string `json:"branch_prefix"`  // e.g. "feature/"
	PinnedKeys    []string `json:"pinned_keys"`  // Locally pinned ticket keys
}

type Issue struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Summary   string    `json:"summary"`
	Status    Status    `json:"status"`
	Priority  Priority  `json:"priority"`
	IssueType IssueType `json:"issue_type"`
	Updated   time.Time `json:"updated"`
	Pinned    bool      `json:"pinned"`
}

type Status struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CategoryKey    string `json:"category_key"` // "new", "indeterminate", "done"
	CategoryColor  string `json:"category_color"`
}

type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   Status `json:"to"`
}

5. Implementation Roadmap for Coding Agents
Phase 1: Core Client & Security
	1.	Initialize Go module: go mod init jira-edge-rail.
	2.	Implement keyring wrapper to securely store/retrieve Jira API Token.
	3.	Build Jira REST API client supporting:
⚬	Basic Auth with API Token (Email:Token Base64) or OAuth 2.0 PKCE.
⚬	Dynamic transition discovery and dispatch.
⚬	Exponential backoff rate limiter.
Phase 2: Native Cocoa Window Bridge
	1.	Create Cgo wrapper (bridge_darwin.go + bridge_darwin.m).
	2.	Implement NSPanel initialization with NSFloatingWindowLevel and NSWindowCollectionBehaviorCanJoinAllSpaces.
	3.	Add mouse tracking (NSTrackingMouseEntered, NSTrackingMouseExited) with 150ms / 250ms debounce timers to prevent jitter.
Phase 3: UI Rendering with gogpu/ui
	1.	Collapsed View (width: 36px):
⚬	Top settings icon.
⚬	Vertical list of status dot badges (Green = In Progress, Yellow = Review, Gray = To Do).
	2.	Expanded View (width: 360px):
⚬	Search/Filter bar with instant fuzzy matching.
⚬	Pinned / In-Progress Tickets section.
⚬	Assigned Tickets list with custom status dropdown pills.
⚬	Copy helpers: Cmd+C on item copies Key, Cmd+Shift+C copies sanitized Git branch name (feature/PROJ-102-short-summary).
	3.	Settings Overlay:
⚬	Instance URL input.
⚬	Email & API token input with validation check button.
⚬	Custom JQL editor.
Phase 4: Background Polling & State Management
	1.	Dedicated background goroutine syncing with Jira search API every ‭$N$‬ seconds.
	2.	Local optimistic UI updates (when clicking a transition, update UI immediately, roll back on API error).
	3.	System notification / audio tick on assigned ticket update.
6. Edge Cases & Handling Strategies
	1.	Required Fields on Transition (Validators/Screens):
⚬	If a transition requires additional fields (e.g., resolution reason, mandatory comment) and API returns HTTP 400 with field errors:
⚬	Provide an automatic inline fallback: "Transition requires mandatory fields. [Open in Browser]".
	2.	Multi-Monitor Setups:
⚬	Detect the active NSScreen (where the mouse cursor is located) and dock the rail to the rightmost edge of that specific display.
	3.	MacBook Display Notch & Menu Bar:
⚬	Dock along the right edge vertically offset from ‭$Y = 0$‬‭‬ to ‭$Y = \text{ScreenHeight}$‬‭‬, respecting display safe areas.
	4.	Offline / Network Interruption:
⚬	Cache the last valid issue list in memory; display a subtle disconnected dot next to the settings gear.
"""
output_path = "jira-edge-rail-specification.md"
with open(output_path, "w", encoding="utf-8") as f:
f.write(spec_content)
print(f"File successfully created: {output_path}")

```text?code_stdout&code_event_index=1
File successfully created: jira-edge-rail-specification.md
