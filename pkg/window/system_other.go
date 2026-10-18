//go:build !darwin || !cgo

package window

import "errors"

func RegisterHotKeyHandler(cb func())                  {}
func SetGlobalHotKey(enabled bool)                     {}
func ActivateApp()                                     {}
func SetTicketHeader(key, summary, url, branch string) {}

func LaunchAtLoginStatus() LoginItemStatus { return LoginItemUnsupported }

func SetLaunchAtLogin(enabled bool) error {
	return errors.New("launch at login is only supported on macOS")
}

func InitNotifications(cb func(issueKey, instanceID string)) bool { return false }
func Notify(title, body, issueKey, instanceID string)             {}
