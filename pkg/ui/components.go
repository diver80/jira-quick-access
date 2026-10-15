package ui

import "time"

// ToastNotification represents a temporary floating notification pill.
type ToastNotification struct {
	Message   string
	CreatedAt time.Time
	Duration  time.Duration
}

func NewToast(msg string) ToastNotification {
	return ToastNotification{
		Message:   msg,
		CreatedAt: time.Now(),
		Duration:  3 * time.Second,
	}
}

func (t *ToastNotification) IsActive() bool {
	return t.Message != "" && time.Since(t.CreatedAt) < t.Duration
}
