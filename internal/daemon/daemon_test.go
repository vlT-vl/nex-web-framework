package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("NEX_TEST_DETACH") == "1" {
		MaybeDetach("probe", "1.0.0", "")
		fmt.Println("continued")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runDetachProbe(t *testing.T, extra ...string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable)
	command.Env = append([]string{"PATH=" + os.Getenv("PATH"), "NEXWEB_STATE_DIR=" + t.TempDir(), "NEX_TEST_DETACH=1"}, extra...)
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		t.Fatalf("probe: %v", err)
	}
	return output.String()
}

func TestProcessLaunchedByUpdaterDoesNotDetach(t *testing.T) {
	if !Enabled() {
		t.Skip("daemon disabled in dev builds")
	}
	if out := runDetachProbe(t, "NEX_UPDATER_HEALTH="+t.TempDir()+"/health"); strings.TrimSpace(out) != "continued" {
		t.Fatalf("a process started by the updater helper must keep running in place: %q", out)
	}
	if out := runDetachProbe(t, "_NEXWEB_CHILD=1"); strings.TrimSpace(out) != "continued" {
		t.Fatalf("daemon child must keep running in place: %q", out)
	}
}

func TestPlainLaunchDetaches(t *testing.T) {
	if !Enabled() {
		t.Skip("daemon disabled in dev builds")
	}
	if out := runDetachProbe(t); !strings.Contains(out, "started (PID") || strings.Contains(out, "continued") {
		t.Fatalf("a plain launch must detach and exit: %q", out)
	}
}
