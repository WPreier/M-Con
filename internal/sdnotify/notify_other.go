//go:build !linux

package sdnotify

// Ready is a no-op outside Linux.
func Ready() {}

// Watchdog is a no-op outside Linux.
func Watchdog() {}

// Stopping is a no-op outside Linux.
func Stopping() {}
