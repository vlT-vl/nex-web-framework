//go:build dev

package daemon

// Enabled reports whether daemon mode is active (disabled in dev builds).
func Enabled() bool { return false }
