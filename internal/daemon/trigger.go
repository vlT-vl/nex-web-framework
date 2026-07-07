//go:build !dev

package daemon

// Enabled reports whether daemon mode is active (release builds).
func Enabled() bool { return true }
