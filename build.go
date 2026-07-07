//go:build ignore

// Build orchestrator for nex-web.
// Usage: go run build.go [command] [flags]
//
//	build         (default) build frontend then all target binaries
//	release       optimized build (-s -w)
//	obfuscate     release + garble code obfuscation
//	requirements  show and validate host toolchain requirements
//	doctor        alias for requirements
//	clean         remove release/ and frontend/dist contents
//
// Flags:
//
//	--plain  skip garble and use go build directly
//
// No CGO required: cross-compilation works from any host to any target.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"nex-web/config"
)

const (
	frontendDir = "frontend"
	mainPkg     = "."
	releaseRoot = "release"
)

var (
	outDir          = releaseRoot
	scriptEnvValues = map[string]string{}
)

type buildTarget struct {
	GOOS   string
	GOARCH string
}

func main() {
	envFile := os.Getenv("NEXWEB_ENV_FILE")
	if envFile == "" {
		envFile = ".env"
	}
	scriptEnvValues = config.Read(envFile)

	args := os.Args[1:]
	cmd := "build"
	obfuscate := true
	filtered := args[:0]
	for _, a := range args {
		if a == "--plain" || a == "-plain" {
			obfuscate = false
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered
	if len(args) > 0 {
		cmd = args[0]
	}
	if cmd == "obfuscate" {
		cmd = "release"
		obfuscate = true
	}

	var err error
	switch cmd {
	case "build":
		err = doBuild(false, obfuscate)
	case "release":
		err = doBuild(true, obfuscate)
	case "requirements", "doctor":
		err = doRequirements()
	case "clean":
		err = doClean()
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		fmt.Fprintln(os.Stderr, "commands: build | release | obfuscate | requirements | doctor | clean")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func doBuild(release, obfuscate bool) error {
	appName, err := appNameFromMain()
	if err != nil {
		return err
	}
	appID := appIDFromName(appName)
	releaseName, err := releaseNameFromVersion()
	if err != nil {
		return err
	}
	outDir = filepath.Join(releaseRoot, releaseName)

	if err := ensureGoModules(); err != nil {
		return err
	}
	if err := buildFrontend(); err != nil {
		return err
	}
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	tool := "go"
	buildPrefix := []string{"build"}
	garbleScope := ""
	if obfuscate {
		garblePath, err := ensureGarble()
		if err != nil {
			return err
		}
		modulePath, err := modulePathFromGoMod()
		if err != nil {
			return err
		}
		garbleScope = modulePath + "," + modulePath + "/..."
		tool = garblePath
		buildPrefix = []string{"-literals", "-seed=random", "build"}
		fmt.Printf("→ using garble for obfuscation (GOGARBLE=%s)\n", garbleScope)
	}

	tmpDir, err := os.MkdirTemp("", "nexweb-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	for _, target := range buildTargets() {
		fmt.Printf("→ target %s/%s\n", target.GOOS, target.GOARCH)
		binaryPath, err := buildTargetBinary(tmpDir, tool, buildPrefix, target, release, appID, garbleScope)
		if err != nil {
			return err
		}
		if err := packagePlatformApp(binaryPath, appName, appID, target); err != nil {
			return err
		}
	}
	return nil
}

// buildTargets returns all supported targets. Because nex-web uses CGO_ENABLED=0,
// cross-compilation works from any host without a native C toolchain.
func buildTargets() []buildTarget {
	return []buildTarget{
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "windows", GOARCH: "amd64"},
	}
}

func buildTargetBinary(tmpDir, tool string, buildPrefix []string, target buildTarget, release bool, appID, garbleScope string) (string, error) {
	targetDir := filepath.Join(tmpDir, target.GOOS+"-"+target.GOARCH)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	outName := appID
	if target.GOOS == "windows" {
		outName += ".exe"
	}
	out := filepath.Join(targetDir, outName)

	ldParts := []string{}
	if release {
		ldParts = append(ldParts, "-s", "-w")
	}
	ldflags := strings.Join(ldParts, " ")

	buildArgs := append([]string{}, buildPrefix...)
	buildArgs = append(buildArgs, "-trimpath")
	if ldflags != "" {
		buildArgs = append(buildArgs, "-ldflags", ldflags)
	}
	buildArgs = append(buildArgs, "-o", out, mainPkg)

	fmt.Printf("→ %s/%s %s %s\n", target.GOOS, target.GOARCH, filepath.Base(tool), strings.Join(buildArgs, " "))
	if err := runCmd(".", targetEnv(target, garbleScope), tool, buildArgs...); err != nil {
		return "", fmt.Errorf("build %s/%s: %w", target.GOOS, target.GOARCH, err)
	}
	return out, nil
}

func targetEnv(target buildTarget, garbleScope string) []string {
	env := cleanEnv(os.Environ(), "GOOS", "GOARCH", "CGO_ENABLED", "GOGARBLE")
	env = append(env,
		"GOOS="+target.GOOS,
		"GOARCH="+target.GOARCH,
		"CGO_ENABLED=0",
	)
	if garbleScope != "" {
		env = append(env, "GOGARBLE="+garbleScope)
	}
	return env
}

// packagePlatformApp creates a per-platform release folder with the binary and extras.
func packagePlatformApp(binaryPath, appName, appID string, target buildTarget) error {
	switch target.GOOS {
	case "darwin":
		return packageDarwinApp(binaryPath, appID, target)
	case "linux":
		return packageLinuxApp(binaryPath, appID, target)
	case "windows":
		return packageWindowsApp(binaryPath, appName, appID)
	default:
		return packageGenericApp(binaryPath, appID, target)
	}
}

func packageDarwinApp(binaryPath, appID string, target buildTarget) error {
	dirName := fmt.Sprintf("%s-%s-%s", appID, target.GOOS, target.GOARCH)
	appDir := filepath.Join(outDir, dirName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(appDir, appID)
	if err := copyFile(binaryPath, dst); err != nil {
		return err
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return err
	}
	fmt.Printf("→ packaged %s\n", appDir)
	return nil
}

func packageLinuxApp(binaryPath, appID string, target buildTarget) error {
	appDir := filepath.Join(outDir, fmt.Sprintf("%s-linux-%s", appID, target.GOARCH))
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(appDir, appID)
	if err := copyFile(binaryPath, dst); err != nil {
		return err
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return err
	}
	fmt.Printf("→ packaged %s\n", appDir)
	return nil
}

func packageWindowsApp(binaryPath, appName, appID string) error {
	appDir := filepath.Join(outDir, appID+"-windows-amd64")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	exeName := appID + ".exe"
	if err := copyFile(binaryPath, filepath.Join(appDir, exeName)); err != nil {
		return err
	}
	manifest := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="0.1.0.0" processorArchitecture="*" name="dev.vlt.nex-web" type="win32"/>
  <description>%s</description>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>
`, xmlEscape(appName))
	if err := os.WriteFile(filepath.Join(appDir, exeName+".manifest"), []byte(manifest), 0o644); err != nil {
		return err
	}
	fmt.Printf("→ packaged %s\n", appDir)
	return nil
}

func packageGenericApp(binaryPath, appID string, target buildTarget) error {
	dirName := fmt.Sprintf("%s-%s-%s", appID, target.GOOS, target.GOARCH)
	appDir := filepath.Join(outDir, dirName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(appDir, appID)
	if err := copyFile(binaryPath, dst); err != nil {
		return err
	}
	_ = os.Chmod(dst, 0o755)
	fmt.Printf("→ packaged %s\n", appDir)
	return nil
}

// doRequirements prints the requirement checklist for nex-web builds.
func doRequirements() error {
	targets := buildTargets()
	fmt.Println("nex-web build requirements")
	fmt.Printf("host: %s/%s\n\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println("targets (all built from any host — CGO_ENABLED=0):")
	for _, t := range targets {
		fmt.Printf("  - %s/%s\n", t.GOOS, t.GOARCH)
	}
	fmt.Println()
	fmt.Println("expected host tools:")
	fmt.Println("  - Go 1.26.4+")
	fmt.Println("  - Node 18+ with npm")
	fmt.Println("  - garble on PATH, or network access for: go install mvdan.cc/garble@latest")
	fmt.Println()

	ok := true

	goVer, err := goEnv("GOVERSION")
	if err != nil {
		fmt.Printf("  go:     ERROR (%v)\n", err)
		ok = false
	} else {
		fmt.Printf("  go:     %s  ✓\n", goVer)
	}

	if npmPath, err := exec.LookPath("npm"); err != nil {
		fmt.Println("  npm:    NOT FOUND — install Node.js 18+")
		ok = false
	} else {
		fmt.Printf("  npm:    %s  ✓\n", npmPath)
	}

	if garblePath, err := exec.LookPath("garble"); err != nil {
		fmt.Println("  garble: not found — will be installed automatically on first obfuscated build")
	} else {
		fmt.Printf("  garble: %s  ✓\n", garblePath)
	}

	fmt.Println()
	if !ok {
		fmt.Fprintln(os.Stderr, "one or more requirements are missing")
		return fmt.Errorf("requirements not satisfied")
	}
	fmt.Println("OK: all build requirements satisfied")
	return nil
}

func ensureGoModules() error {
	return runCmd(".", nil, "go", "mod", "tidy")
}

func ensureGarble() (string, error) {
	if garblePath, err := exec.LookPath("garble"); err == nil {
		return garblePath, nil
	}
	fmt.Println("→ garble not found; installing mvdan.cc/garble@latest")
	if err := runCmd(".", nil, "go", "install", "mvdan.cc/garble@latest"); err != nil {
		return "", err
	}
	if garblePath, err := exec.LookPath("garble"); err == nil {
		return garblePath, nil
	}
	garblePath, err := goBinPath("garble")
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(garblePath); err != nil {
		return "", fmt.Errorf("garble installed but not found at %s", garblePath)
	}
	return garblePath, nil
}

func goBinPath(name string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	gobin, err := goEnv("GOBIN")
	if err != nil {
		return "", err
	}
	if gobin == "" {
		gopath, err := goEnv("GOPATH")
		if err != nil {
			return "", err
		}
		if gopath == "" {
			return "", fmt.Errorf("go env GOPATH is empty")
		}
		gobin = filepath.Join(gopath, "bin")
	}
	return filepath.Join(gobin, name), nil
}

func goEnv(key string) (string, error) {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func modulePathFromGoMod() (string, error) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			modulePath := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			if modulePath != "" {
				return modulePath, nil
			}
		}
	}
	return "", fmt.Errorf("go.mod module path not found")
}

func buildFrontend() error {
	if _, err := os.Stat(filepath.Join(frontendDir, "node_modules")); os.IsNotExist(err) {
		if err := runCmd(frontendDir, nil, "npm", "install"); err != nil {
			return err
		}
	}
	return runCmd(frontendDir, nil, "npm", "run", "build")
}

// appNameFromMain reads NEXWEB_APP_NAME from .env first, then falls back to
// parsing main.go. It understands both string literals and envDefault(...) calls.
func appNameFromMain() (string, error) {
	if name := strings.TrimSpace(scriptEnvDefault("NEXWEB_APP_NAME", "")); name != "" {
		return name, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		return "", fmt.Errorf("reading app name from main.go: %w", err)
	}
	name := ""
	ast.Inspect(f, func(n ast.Node) bool {
		if name != "" {
			return false
		}
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Name" {
			return true
		}
		if v, ok := stringExprValue(kv.Value); ok {
			name = strings.TrimSpace(v)
		}
		return false
	})
	if name == "" {
		return "", fmt.Errorf("main.go nexweb.Config Name is missing or not a string literal")
	}
	return name, nil
}

func stringExprValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if ok && lit.Kind == token.STRING {
		unquoted, err := strconv.Unquote(lit.Value)
		return unquoted, err == nil
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	fun, ok := call.Fun.(*ast.Ident)
	if !ok || fun.Name != "envDefault" || len(call.Args) < 2 {
		return "", false
	}
	key, ok := stringExprValue(call.Args[0])
	if !ok {
		return "", false
	}
	fallback, ok := stringExprValue(call.Args[1])
	if !ok {
		return "", false
	}
	return scriptEnvDefault(key, fallback), true
}

func releaseNameFromVersion() (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "internal/meta/meta.go", nil, 0)
	if err != nil {
		return "", fmt.Errorf("reading release version from internal/meta/meta.go: %w", err)
	}
	values := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || (gen.Tok != token.VAR && gen.Tok != token.CONST) {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, vname := range vs.Names {
				if vname.Name != "Version" && vname.Name != "Build" {
					continue
				}
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err == nil {
					values[vname.Name] = strings.TrimSpace(v)
				}
			}
		}
	}
	version := sanitizeReleasePart(values["Version"])
	build := sanitizeReleasePart(values["Build"])
	if version == "" || build == "" {
		return "", fmt.Errorf("internal/meta/meta.go must define Version and Build string vars")
	}
	return version + "-" + build, nil
}

func sanitizeReleasePart(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '-' || r == '_'
		if ok {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".-_")
}

func scriptEnvDefault(key, fallback string) string {
	if v := strings.TrimSpace(scriptEnvValues[key]); v != "" {
		return v
	}
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func appIDFromName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	dash := false
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "nex-web"
	}
	return out
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return strings.ReplaceAll(s, "'", "&apos;")
}

func cleanEnv(env []string, keys ...string) []string {
	block := map[string]bool{}
	for _, k := range keys {
		block[k] = true
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || block[key] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func doClean() error {
	_ = os.RemoveAll(releaseRoot)
	entries, _ := os.ReadDir(filepath.Join(frontendDir, "dist"))
	placeholder := []string{"index.html"}
	for _, e := range entries {
		if !slices.Contains(placeholder, e.Name()) {
			_ = os.RemoveAll(filepath.Join(frontendDir, "dist", e.Name()))
		}
	}
	fmt.Println("cleaned release/ and frontend/dist (kept placeholder)")
	return nil
}

func runCmd(dir string, env []string, name string, args ...string) error {
	fmt.Printf("→ (%s) %s %s\n", dir, name, strings.Join(args, " "))
	c := exec.Command(name, args...)
	c.Dir = dir
	if env != nil {
		c.Env = env
	}
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
