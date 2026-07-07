// Package system: OS-agnostic helpers shared by the per-platform osinfo_*.go
// files. No exec.Command, no platform-specific imports.
package system

import (
	"strconv"
	"strings"
	"time"
)

// charsToString converts a fixed-size NUL-terminated C char array (as returned
// by syscalls such as uname(2) or statfs(2)) into a Go string. Works for both
// signed (int8) and unsigned (byte) array element types, since the underlying
// C `char` signedness varies by platform/architecture.
func charsToString[T ~int8 | ~byte](b []T) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}

func percentOf(used, total uint64) int {
	if total == 0 {
		return 0
	}
	return int(used * 100 / total)
}

// durationString mirrors the historical `(time.Duration(seconds) *
// time.Second).String()` formatting used for uptime figures.
func durationString(seconds float64) string {
	return (time.Duration(seconds) * time.Second).String()
}

// unescapeMountField decodes the octal escapes (\040 for space, \011 tab,
// \012 newline, \134 backslash) that the kernel uses for device/mountpoint
// fields in /proc/mounts.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseInt(s[i+1:i+4], 8, 16); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
