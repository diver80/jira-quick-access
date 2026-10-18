//go:build darwin && cgo

package window

/*
#cgo LDFLAGS: -framework Carbon -framework ServiceManagement -framework UserNotifications
#include <stdlib.h>

extern void JQASetGlobalHotKey(int enabled);
extern void JQAActivateApp(void);
extern int JQALoginItemStatus(void);
extern char *JQASetLoginItem(int enabled);
extern int JQANotificationsInit(void);
extern void JQANotify(const char *title, const char *body, const char *issueKey, const char *instanceID);
extern void JQAHeaderSetTicket(const char *key, const char *summary, const char *url, const char *branch);
*/
import "C"

import (
	"errors"
	"sync"
	"unsafe"
)

var (
	systemMu            sync.Mutex
	hotKeyCallback      func()
	notificationHandler func(issueKey, instanceID string)
)

//export goHotKeyPressed
func goHotKeyPressed() {
	systemMu.Lock()
	cb := hotKeyCallback
	systemMu.Unlock()
	if cb != nil {
		cb()
	}
}

//export goNotificationClicked
func goNotificationClicked(issueKey, instanceID *C.char) {
	key, inst := C.GoString(issueKey), C.GoString(instanceID)
	systemMu.Lock()
	cb := notificationHandler
	systemMu.Unlock()
	if cb != nil {
		cb(key, inst)
	}
}

// RegisterHotKeyHandler sets the function run when the global hotkey is pressed.
func RegisterHotKeyHandler(cb func()) {
	systemMu.Lock()
	hotKeyCallback = cb
	systemMu.Unlock()
}

// SetGlobalHotKey registers or removes the system-wide ⌥⌘J shortcut.
func SetGlobalHotKey(enabled bool) {
	C.JQASetGlobalHotKey(boolToC(enabled))
}

// ActivateApp brings the HUD to the front with keyboard focus.
func ActivateApp() {
	C.JQAActivateApp()
}

// LaunchAtLoginStatus reports whether the app is registered as a login item.
func LaunchAtLoginStatus() LoginItemStatus {
	switch C.JQALoginItemStatus() {
	case 1:
		return LoginItemEnabled
	case 2:
		return LoginItemNeedsApproval
	case 0:
		return LoginItemDisabled
	}
	return LoginItemUnsupported
}

// SetLaunchAtLogin registers or removes the app as a login item.
func SetLaunchAtLogin(enabled bool) error {
	if msg := C.JQASetLoginItem(boolToC(enabled)); msg != nil {
		defer C.free(unsafe.Pointer(msg))
		return errors.New(C.GoString(msg))
	}
	return nil
}

// InitNotifications asks for notification permission and routes clicks to cb.
// It reports false when notifications are unavailable (not running as an app bundle).
func InitNotifications(cb func(issueKey, instanceID string)) bool {
	systemMu.Lock()
	notificationHandler = cb
	systemMu.Unlock()
	return C.JQANotificationsInit() != 0
}

// Notify shows a notification; clicking it reports issueKey and instanceID.
func Notify(title, body, issueKey, instanceID string) {
	cTitle, cBody := C.CString(title), C.CString(body)
	cKey, cInst := C.CString(issueKey), C.CString(instanceID)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cBody))
	defer C.free(unsafe.Pointer(cKey))
	defer C.free(unsafe.Pointer(cInst))
	C.JQANotify(cTitle, cBody, cKey, cInst)
}

// SetTicketHeader updates the header above the ticket panel. An empty url hides
// the link actions, an empty branch the branch action.
func SetTicketHeader(key, summary, url, branch string) {
	cKey, cSummary := C.CString(key), C.CString(summary)
	cURL, cBranch := C.CString(url), C.CString(branch)
	defer C.free(unsafe.Pointer(cKey))
	defer C.free(unsafe.Pointer(cSummary))
	defer C.free(unsafe.Pointer(cURL))
	defer C.free(unsafe.Pointer(cBranch))
	C.JQAHeaderSetTicket(cKey, cSummary, cURL, cBranch)
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
