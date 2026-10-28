package updater

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeScript(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPreflight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts are not executable on windows")
	}
	tests := []struct {
		name     string
		script   string
		mode     os.FileMode
		readonly bool
		wantErr  string
	}{
		{name: "ok", script: "exit 0", mode: 0755},
		{name: "self-check predates the flag", script: "echo 'flag provided but not defined' >&2; exit 2", mode: 0755},
		{name: "self-check non-zero", script: "echo boom >&2; exit 3", mode: 0755, wantErr: "self-check failed"},
		{name: "staged not executable", script: "exit 0", mode: 0644, wantErr: "self-check failed"},
		{name: "install directory read-only", script: "exit 0", mode: 0755, readonly: true, wantErr: "not writable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.readonly && os.Geteuid() == 0 {
				t.Skip("root ignores directory permissions")
			}
			installDir := t.TempDir()
			target := writeScript(t, installDir, "app", "exit 0", 0755)
			staged := writeScript(t, t.TempDir(), "new", test.script, test.mode)
			if test.readonly {
				if err := os.Chmod(installDir, 0500); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(installDir, 0700)
			}
			err := preflight(context.Background(), target, staged, "", 1)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("preflight() error=%v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("preflight() error=%v want substring %q", err, test.wantErr)
			}
			if _, statErr := os.Stat(target); statErr != nil {
				t.Fatalf("target must stay untouched: %v", statErr)
			}
		})
	}
}

func TestPreflightLayoutMismatch(t *testing.T) {
	installDir := t.TempDir()
	target := filepath.Join(installDir, "app")
	if err := os.WriteFile(target, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	staged := t.TempDir()
	if err := preflight(context.Background(), target, staged, "", 1); err == nil || !strings.Contains(err.Error(), "layout") {
		t.Fatalf("preflight() error=%v", err)
	}
}

func newTestJob(t *testing.T) (helperJob, string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Dir(executable)
	stagingRoot, err := os.MkdirTemp(workDir, "nex-helper-stage-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(stagingRoot) })
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "current")
	staged := filepath.Join(stagingRoot, "new")
	if err := copyExecutable(executable, staged); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "updater-install-target"), []byte(target), 0600); err != nil {
		t.Fatal(err)
	}
	job := helperJob{AppID: "test-app", StateDir: stateDir, WorkDir: workDir, StagingRoot: stagingRoot, Staged: staged, Target: target, Backup: filepath.Join(targetDir, ".current.nex-backup-test"), ParentPID: 2147483647, HealthTimeout: time.Second}
	jobPath := filepath.Join(workDir, "job-test-"+safeName(strings.ReplaceAll(t.Name(), "/", "-"))+".json")
	return job, jobPath, target
}

func writeTestJob(t *testing.T, job helperJob, jobPath string) {
	t.Helper()
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(jobPath) })
}

func TestHelperBackupRenameFails(t *testing.T) {
	job, jobPath, _ := newTestJob(t)
	writeTestJob(t, job, jobPath)
	t.Setenv("NEX_TEST_FAKE_APP", "1")
	t.Setenv("NEX_TEST_FAKE_MODE", "exit")
	t.Setenv("NEX_TEST_FAKE_MARKER", filepath.Join(t.TempDir(), "old-running"))
	if err := runHelper(jobPath); err != nil {
		t.Fatal(err)
	}
	if result := readResultFile(t, job.StateDir); result.Status != "rolledBack:swap" {
		t.Fatalf("result status=%q", result.Status)
	}
	if _, err := os.Stat(job.Backup); !os.IsNotExist(err) {
		t.Fatalf("no backup must exist: %v", err)
	}
}

func TestRolledBackReleaseIsRemembered(t *testing.T) {
	job, jobPath, _ := newTestJob(t)
	job.Version = "1.2.3"
	job.Build = "R011226"
	writeTestJob(t, job, jobPath)
	t.Setenv("NEX_TEST_FAKE_APP", "1")
	t.Setenv("NEX_TEST_FAKE_MODE", "exit")
	t.Setenv("NEX_TEST_FAKE_MARKER", filepath.Join(t.TempDir(), "old-running"))
	if err := runHelper(jobPath); err != nil {
		t.Fatal(err)
	}
	host := &fakeHost{appID: "test-app", version: "0.1.0", build: "R010126", state: job.StateDir, mode: InstallSelf}
	u := New(Config{SourceURL: "https://github.com/owner/repo"}, host)
	if !u.wasRolledBack(&Release{Version: "1.2.3", Build: "R011226"}) {
		t.Fatal("the rolled back release must be remembered")
	}
	if u.wasRolledBack(&Release{Version: "1.2.4", Build: "R011226"}) || u.wasRolledBack(&Release{Version: "1.2.3", Build: "R021226"}) {
		t.Fatal("only the exact rolled back release must be flagged")
	}
}

func TestHelperParentNeverExits(t *testing.T) {
	job, jobPath, target := newTestJob(t)
	if err := os.WriteFile(target, []byte("old build"), 0700); err != nil {
		t.Fatal(err)
	}
	job.ParentPID = os.Getpid()
	job.ParentWait = 200 * time.Millisecond
	writeTestJob(t, job, jobPath)
	if err := runHelper(jobPath); err != nil {
		t.Fatal(err)
	}
	if result := readResultFile(t, job.StateDir); result.Status != "rolledBack:parent_timeout" {
		t.Fatalf("result status=%q", result.Status)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old build" {
		t.Fatalf("target must be untouched: %q, %v", data, err)
	}
	if _, err := os.Stat(job.Backup); !os.IsNotExist(err) {
		t.Fatalf("no backup must exist: %v", err)
	}
}

type gateHost struct {
	*fakeHost
	entered chan struct{}
	proceed chan struct{}
}

func (h *gateHost) Authorize(string) error {
	h.entered <- struct{}{}
	<-h.proceed
	return errors.New("denied")
}

func TestConcurrentOperationsAreGuarded(t *testing.T) {
	host := &gateHost{fakeHost: &fakeHost{appID: "test-app", version: "0.1.0", build: "R010126", state: t.TempDir(), mode: InstallSelf}, entered: make(chan struct{}, 8), proceed: make(chan struct{})}
	u := New(Config{SourceURL: "https://github.com/owner/repo"}, host)
	done := make(chan error, 1)
	go func() {
		_, err := u.Check(context.Background(), true)
		done <- err
	}()
	<-host.entered
	second := make(chan State, 1)
	go func() {
		state, _ := u.Check(context.Background(), true)
		second <- state
	}()
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("a concurrent check must return immediately instead of blocking")
	}
	if err := u.Download(context.Background()); ErrorCode(err) != "busy" {
		t.Fatalf("Download() while checking: %v", err)
	}
	if err := u.Install(context.Background()); ErrorCode(err) != "busy" {
		t.Fatalf("Install() while checking: %v", err)
	}
	close(host.proceed)
	if err := <-done; ErrorCode(err) != "unauthorized" {
		t.Fatalf("first Check() error=%v", err)
	}
	if err := u.Download(context.Background()); ErrorCode(err) == "busy" {
		t.Fatal("the guard must be released after the operation ends")
	}
}

func TestSetSourceIfUnset(t *testing.T) {
	host := &fakeHost{appID: "test-app", version: "0.1.0", build: "R010126", state: t.TempDir(), mode: InstallSelf}
	u := New(Config{}, host)
	if u.State().Configured {
		t.Fatal("must start unconfigured")
	}
	u.SetSourceIfUnset("https://github.com/owner/repo")
	if state := u.State(); !state.Configured || state.Source != "github" {
		t.Fatalf("state after first set: %+v", state)
	}
	u.SetSourceIfUnset("https://example.com/feed.json")
	if state := u.State(); state.Source != "github" {
		t.Fatalf("an already configured source must win: %+v", state)
	}
	configured := New(Config{SourceURL: "https://example.com/feed.json"}, host)
	configured.SetSourceIfUnset("https://github.com/owner/repo")
	if state := configured.State(); state.Source != "feed" {
		t.Fatalf("configured source must win over the legacy parameter: %+v", state)
	}
}
