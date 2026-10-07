package meta

import (
	"os"
	"runtime/debug"
	"strings"
)

const Name = "nex-web"

var (
	Version = "0.1.1"
	Build   = "R071026"
	Updated = "7 Ottobre 2026"
	Author  = "© 2026 vlT di Veronesi Lorenzo"
)

func Info() map[string]any {
	return map[string]any{
		"name":    Name,
		"version": Version,
		"build":   Build,
		"updated": Updated,
		"author":  Author,
		"id":      Name + "@" + Version,
		"stack":   Stack(),
	}
}

func Stack() map[string]string {
	out := map[string]string{
		"nex-web": Name + "@" + Version + "-" + Build,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if bi.GoVersion != "" {
			out["go"] = strings.TrimPrefix(bi.GoVersion, "go")
		}
		for _, dep := range bi.Deps {
			addDependency(out, dep.Path, dep.Version)
			if dep.Replace != nil {
				addDependency(out, dep.Replace.Path, dep.Replace.Version)
			}
		}
	}
	for path, version := range readGoModDependencies("go.mod") {
		addDependency(out, path, version)
	}
	return out
}

func addDependency(out map[string]string, path, version string) {
	if version == "" || version == "(devel)" {
		return
	}
	short := path
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	out[short] = version
}

func readGoModDependencies(path string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.Contains(fields[0], ".") &&
			!strings.HasPrefix(fields[0], "go") && !strings.HasPrefix(fields[0], "//") {
			out[fields[0]] = fields[1]
		}
	}
	return out
}
