package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("NEX_TEST_FAKE_APP") == "1" {
		marker := os.Getenv("NEX_TEST_FAKE_MARKER")
		if os.Getenv(healthEnvironment) != "" {
			switch os.Getenv("NEX_TEST_FAKE_MODE") {
			case "exit":
				os.Exit(17)
			case "hang":
				time.Sleep(10 * time.Second)
				os.Exit(18)
			case "crash":
				time.Sleep(150 * time.Millisecond)
				os.Exit(3)
			case "confirm":
				_ = os.WriteFile(marker, []byte("new-running"), 0600)
				_, _ = Confirm()
				time.Sleep(2 * time.Second)
				os.Exit(0)
			}
		}
		if marker != "" {
			_ = os.WriteFile(marker, []byte("old-running"), 0600)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestHelperRollbackFailures(t *testing.T) {
	tests := []struct {
		name          string
		mode          string
		missingStaged bool
		result        string
		timeout       time.Duration
	}{
		{name: "new build exits immediately", mode: "exit", result: "rolledBack:early_exit", timeout: 2 * time.Second},
		{name: "new build crashes after start before confirm", mode: "crash", result: "rolledBack:early_exit", timeout: 3 * time.Second},
		{name: "new build never confirms", mode: "hang", result: "rolledBack:timeout", timeout: 150 * time.Millisecond},
		{name: "staged rename fails", mode: "exit", missingStaged: true, result: "rolledBack:swap", timeout: time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			workDir := filepath.Dir(executable)
			stagingRoot, err := os.MkdirTemp(workDir, "nex-helper-stage-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(stagingRoot)
			targetDir := t.TempDir()
			target := filepath.Join(targetDir, "current")
			if err := copyExecutable(executable, target); err != nil {
				t.Fatal(err)
			}
			staged := filepath.Join(stagingRoot, "new")
			if !test.missingStaged {
				if err := copyExecutable(executable, staged); err != nil {
					t.Fatal(err)
				}
			}
			stateDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(stateDir, "updater-install-target"), []byte(target), 0600); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(targetDir, ".current.nex-backup-test")
			job := helperJob{AppID: "test-app", StateDir: stateDir, WorkDir: workDir, StagingRoot: stagingRoot, Staged: staged, Target: target, Backup: backup, ParentPID: 2147483647, HealthTimeout: test.timeout}
			jobPath := filepath.Join(workDir, "job-test-"+safeName(strings.ReplaceAll(t.Name(), "/", "-"))+".json")
			data, err := json.Marshal(job)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(jobPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(jobPath)
			marker := filepath.Join(t.TempDir(), "old-running")
			t.Setenv("NEX_TEST_FAKE_APP", "1")
			t.Setenv("NEX_TEST_FAKE_MODE", test.mode)
			t.Setenv("NEX_TEST_FAKE_MARKER", marker)
			if err := runHelper(jobPath); err != nil {
				t.Fatal(err)
			}
			waitForFile(t, marker)
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("previous target was not restored: %v", err)
			}
			result := readResultFile(t, stateDir)
			if result.Status != test.result {
				t.Fatalf("result status=%q want=%q", result.Status, test.result)
			}
		})
	}
}

func TestSettingsDefaultsAndPersistence(t *testing.T) {
	host := &fakeHost{appID: "test-app", version: "0.1.0", build: "R010126", state: t.TempDir(), mode: InstallSelf}
	first := New(Config{SourceURL: "https://github.com/owner/repo"}, host)
	if got := first.State().Settings; !got.AutoCheck || got.AutoDownload || got.IncludePrerelease {
		t.Fatalf("default settings=%+v", got)
	}
	if err := first.SetSettings(Settings{}); err != nil {
		t.Fatal(err)
	}
	second := New(Config{SourceURL: "https://github.com/owner/repo"}, host)
	if got := second.State().Settings; got.AutoCheck {
		t.Fatalf("persisted settings were not honoured: %+v", got)
	}
}

func TestHelperHappyPath(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Dir(executable)
	stagingRoot, err := os.MkdirTemp(workDir, "nex-helper-stage-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stagingRoot)
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "current")
	if err := os.WriteFile(target, []byte("old build"), 0700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(stagingRoot, "new")
	if err := copyExecutable(executable, staged); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "updater-install-target"), []byte(target), 0600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(targetDir, ".current.nex-backup-test")
	job := helperJob{AppID: "test-app", StateDir: stateDir, WorkDir: workDir, StagingRoot: stagingRoot, Staged: staged, Target: target, Backup: backup, ParentPID: 2147483647, HealthTimeout: 5 * time.Second}
	jobPath := filepath.Join(workDir, "job-test-happy.json")
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(jobPath)
	if err := os.WriteFile(filepath.Join(stateDir, rejectedFile), []byte(`{"version":"0.0.1","build":"R010126"}`), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "new-running")
	t.Setenv("NEX_TEST_FAKE_APP", "1")
	t.Setenv("NEX_TEST_FAKE_MODE", "confirm")
	t.Setenv("NEX_TEST_FAKE_MARKER", marker)
	if err := runHelper(jobPath); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, marker)
	result := readResultFile(t, stateDir)
	if result.Status != "updated" {
		t.Fatalf("result status=%q want=updated", result.Status)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed after a confirmed update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, rejectedFile)); !os.IsNotExist(err) {
		t.Fatalf("rejected marker should be cleared after a confirmed update: %v", err)
	}
	if _, err := os.Stat(stagingRoot); !os.IsNotExist(err) {
		t.Fatalf("staging should be removed after a confirmed update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "updater-journal.json")); !os.IsNotExist(err) {
		t.Fatalf("journal should be removed after a confirmed update: %v", err)
	}
}

func TestRecoverBetweenRenames(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "current")
	if err := copyExecutable(executable, target); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(targetDir, ".current.nex-backup-test")
	if err := os.Rename(target, backup); err != nil {
		t.Fatal(err)
	}
	stagingRoot, err := os.MkdirTemp(filepath.Dir(executable), "nex-recover-stage-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stagingRoot)
	staged := filepath.Join(stagingRoot, "new")
	if err := copyExecutable(executable, staged); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	job := helperJob{AppID: "test-app", StateDir: stateDir, WorkDir: filepath.Dir(executable), StagingRoot: stagingRoot, Staged: staged, Target: target, Backup: backup, ParentPID: 2147483647, HealthTimeout: time.Second}
	if err := os.WriteFile(filepath.Join(stateDir, "updater-install-target"), []byte(target), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(stateDir, "updater-journal.json"), journal{Phase: "swap", Job: job}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "old-running")
	t.Setenv("NEX_TEST_FAKE_APP", "1")
	t.Setenv("NEX_TEST_FAKE_MODE", "exit")
	t.Setenv("NEX_TEST_FAKE_MARKER", marker)
	if err := Recover(stateDir); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, marker)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target not recovered: %v", err)
	}
	result := readResultFile(t, stateDir)
	if result.Status != "rolledBack:interrupted" {
		t.Fatalf("result status=%q", result.Status)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func readResultFile(t *testing.T, stateDir string) Result {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, "updater-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
