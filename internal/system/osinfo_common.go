package system

import (
	"strconv"
	"strings"
	"time"
)

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

func durationString(seconds float64) string {
	return (time.Duration(seconds) * time.Second).String()
}

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
