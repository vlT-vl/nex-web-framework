// Package config loads .env files.
// Variables prefixed with VITE_ are public (sent to frontend via sys.app.info).
// All other variables are backend-only and never exposed to the frontend.
package config

import (
	"bufio"
	"os"
	"strings"
)

const DefaultPublicPrefix = "VITE_"

type Env struct{}

// Read reads a .env file into a map without mutating process environment.
func Read(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		i := strings.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := unquote(strings.TrimSpace(line[i+1:]))
		if key != "" {
			out[key] = val
		}
	}
	return out
}

// Load reads a .env file (best-effort) and sets variables in the process
// environment without overwriting existing ones.
func Load(path string) *Env {
	for key, val := range Read(path) {
		if _, ok := os.LookupEnv(key); !ok {
			_ = os.Setenv(key, val)
		}
	}
	return &Env{}
}

func (*Env) Get(key string) string { return os.Getenv(key) }
func (*Env) GetDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
func (*Env) Has(key string) bool { _, ok := os.LookupEnv(key); return ok }

// Public returns only variables with the given prefix (e.g. "VITE_"),
// safe to expose to the frontend.
func (*Env) Public(prefix string) map[string]string {
	if prefix == "" {
		prefix = DefaultPublicPrefix
	}
	out := map[string]string{}
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		k := kv[:i]
		if strings.HasPrefix(k, prefix) {
			out[k] = kv[i+1:]
		}
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
