package hostenv

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfCheckExitsBeforeAnythingElse(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "--nex-selfcheck")
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		t.Fatalf("selfcheck exit: %v", err)
	}
	if strings.TrimSpace(output.String()) != `{"selfcheck":true,"protocolVersion":1}` {
		t.Fatalf("selfcheck output: %q", output.String())
	}
}

func TestSupervisedDetection(t *testing.T) {
	none := func(string) bool { return false }
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	tests := []struct {
		name   string
		getenv func(string) string
		exists func(string) bool
		want   bool
	}{
		{"plain shell", env(map[string]string{"XPC_SERVICE_NAME": "0"}), none, false},
		{"docker", env(nil), func(p string) bool { return p == "/.dockerenv" }, true},
		{"podman", env(nil), func(p string) bool { return p == "/run/.containerenv" }, true},
		{"kubernetes", env(map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"}), none, true},
		{"systemd", env(map[string]string{"INVOCATION_ID": "x"}), none, true},
		{"launchd", env(map[string]string{"XPC_SERVICE_NAME": "com.example.app"}), none, true},
		{"app launched from Finder", env(map[string]string{"XPC_SERVICE_NAME": "application.com.example.1"}), none, false},
	}
	for _, test := range tests {
		if got := supervised(test.getenv, test.exists); got != test.want {
			t.Errorf("%s: got %v want %v", test.name, got, test.want)
		}
	}
}

func TestStateDir(t *testing.T) {
	explicit := t.TempDir()
	t.Setenv(StateDirEnv, explicit)
	if got := StateDir("any"); got != explicit {
		t.Fatalf("explicit state dir ignored: %s", got)
	}
	t.Setenv(StateDirEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", home)
	got := StateDir("my/app name")
	if !strings.HasPrefix(got, home) || strings.ContainsAny(filepath.Base(got), " /") {
		t.Fatalf("unexpected fallback state dir: %s", got)
	}
}

func TestJournalRestores(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup")
	if err := os.WriteFile(backup, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(dir, "updater-journal.json")
	t.Setenv(healthEnv, "")
	if journalRestores(dir) {
		t.Fatal("no journal, nothing to restore")
	}
	if err := os.WriteFile(journal, []byte(`{"phase":"swap","job":{"backup":"`+backup+`"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if !journalRestores(dir) {
		t.Fatal("journal with an existing backup must restore")
	}
	t.Setenv(healthEnv, "/tmp/health")
	if journalRestores(dir) {
		t.Fatal("a process launched by the helper must not recover")
	}
	t.Setenv(healthEnv, "")
	os.Remove(backup)
	if journalRestores(dir) {
		t.Fatal("journal without a backup leaves the current binary in place")
	}
}
