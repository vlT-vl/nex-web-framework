package hostenv

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"nex-web/internal/updater"
)

const (
	ChildEnv    = "_NEXWEB_CHILD"
	StateDirEnv = "NEXWEB_STATE_DIR"
	healthEnv   = "NEX_UPDATER_HEALTH"
)

func init() {
	if updater.RunHelperIfRequested() {
		os.Exit(0)
	}
}

func DaemonChild() bool {
	return os.Getenv(ChildEnv) != ""
}

func UpdaterLaunched() bool {
	return os.Getenv(healthEnv) != ""
}

func Supervised() bool {
	return supervised(os.Getenv, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func supervised(getenv func(string) string, exists func(string) bool) bool {
	if exists("/.dockerenv") || exists("/run/.containerenv") {
		return true
	}
	for _, key := range []string{"KUBERNETES_SERVICE_HOST", "INVOCATION_ID", "JOURNAL_STREAM"} {
		if getenv(key) != "" {
			return true
		}
	}
	service := getenv("XPC_SERVICE_NAME")
	return service != "" && service != "0" && !strings.HasPrefix(service, "application.")
}

func StateDir(appID string) string {
	if dir := os.Getenv(StateDirEnv); dir != "" {
		return dir
	}
	name := safeName(appID)
	var candidates []string
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		candidates = append(candidates, filepath.Join(base, name))
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "."+name+"-state"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), name+"-state"))
	for _, candidate := range candidates {
		if writable(candidate) {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}

func writable(dir string) bool {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false
	}
	probe, err := os.CreateTemp(dir, ".probe-")
	if err != nil {
		return false
	}
	path := probe.Name()
	probe.Close()
	os.Remove(path)
	return true
}

func safeName(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			out.WriteRune(r)
		default:
			out.WriteRune('-')
		}
	}
	if out.Len() == 0 {
		return "nex-web"
	}
	return out.String()
}

func Recover(appID string) {
	dir := StateDir(appID)
	relaunches := journalRestores(dir)
	if err := updater.Recover(dir); err != nil {
		log.Printf("%s updater recover: %v", appID, err)
		return
	}
	if relaunches {
		os.Exit(0)
	}
}

func journalRestores(stateDir string) bool {
	if UpdaterLaunched() {
		return false
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "updater-journal.json"))
	if err != nil {
		return false
	}
	var record struct {
		Job struct {
			Backup string `json:"backup"`
		} `json:"job"`
	}
	if json.Unmarshal(data, &record) != nil || record.Job.Backup == "" {
		return false
	}
	_, err = os.Stat(record.Job.Backup)
	return err == nil
}
