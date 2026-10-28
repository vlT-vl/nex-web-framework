package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const helperJobEnvironment = "NEX_UPDATER_JOB"
const healthEnvironment = "NEX_UPDATER_HEALTH"
const selfCheckFlag = "--nex-selfcheck"

func RunHelperIfRequested() bool {
	for _, argument := range os.Args[1:] {
		if argument == selfCheckFlag {
			fmt.Fprintln(os.Stdout, `{"selfcheck":true,"protocolVersion":1}`)
			return true
		}
	}
	jobPath := os.Getenv(helperJobEnvironment)
	if jobPath == "" {
		return false
	}
	_ = runHelper(jobPath)
	return true
}

func Confirm() (bool, error) {
	path := os.Getenv(healthEnvironment)
	if path == "" {
		return false, nil
	}
	if err := os.WriteFile(path, []byte("healthy"), 0600); err != nil {
		return false, coded("state_unavailable", err)
	}
	return true, nil
}

func (u *Updater) Confirm() (bool, error) {
	return Confirm()
}

func runHelper(jobPath string) error {
	data, err := os.ReadFile(jobPath)
	if err != nil {
		return err
	}
	var job helperJob
	if err := json.Unmarshal(data, &job); err != nil {
		return err
	}
	if err := validateJob(jobPath, job); err != nil {
		return err
	}
	journalPath := filepath.Join(job.StateDir, "updater-journal.json")
	healthPath := filepath.Join(job.StateDir, "updater-health-"+fmt.Sprint(os.Getpid()))
	os.Remove(healthPath)
	writePhase := func(phase string) error {
		return writeJSONAtomic(journalPath, journal{Phase: phase, Job: job})
	}
	if err := writePhase("wait"); err != nil {
		return err
	}
	parentWait := job.ParentWait
	if parentWait <= 0 {
		parentWait = 30 * time.Second
	}
	waitDeadline := time.Now().Add(parentWait)
	for processAlive(job.ParentPID) && time.Now().Before(waitDeadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(job.ParentPID) {
		return finishRollback(job, journalPath, "parent_timeout", false)
	}
	if err := writePhase("backup"); err != nil {
		return err
	}
	if err := os.Rename(job.Target, job.Backup); err != nil {
		return finishRollback(job, journalPath, "swap", true)
	}
	if err := writePhase("swap"); err != nil {
		return err
	}
	if err := os.Rename(job.Staged, job.Target); err != nil {
		_ = os.Rename(job.Backup, job.Target)
		return finishRollback(job, journalPath, "swap", true)
	}
	if err := writePhase("launch"); err != nil {
		return err
	}
	newExecutable := launchPath(job.Target, job.LaunchEntry)
	environment := replaceEnvironment(os.Environ(), helperJobEnvironment, "")
	environment = replaceEnvironment(environment, healthEnvironment, healthPath)
	command, err := startDetached(newExecutable, environment)
	if err != nil {
		return rollbackSwapped(job, journalPath, "launch")
	}
	exited := make(chan error, 1)
	go func() {
		exited <- command.Wait()
	}()
	if err := writePhase("health"); err != nil {
		_ = command.Process.Kill()
		return err
	}
	timeout := job.HealthTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-exited:
			return rollbackSwapped(job, journalPath, "early_exit")
		case <-timer.C:
			_ = command.Process.Kill()
			select {
			case <-exited:
			case <-time.After(2 * time.Second):
			}
			return rollbackSwapped(job, journalPath, "timeout")
		case <-ticker.C:
			if _, err := os.Stat(healthPath); err == nil {
				if err := writeResult(job.StateDir, Result{Status: "updated", Time: time.Now().UTC()}); err != nil {
					return err
				}
				os.RemoveAll(job.Backup)
				os.RemoveAll(job.StagingRoot)
				os.Remove(healthPath)
				os.Remove(journalPath)
				os.Remove(jobPath)
				os.Remove(filepath.Join(job.StateDir, rejectedFile))
				cleanupHelper(job)
				return nil
			}
		}
	}
}

func validateJob(jobPath string, job helperJob) error {
	helperExecutable, err := os.Executable()
	if err != nil {
		return err
	}
	helperDir := filepath.Clean(filepath.Dir(helperExecutable))
	if filepath.Clean(job.WorkDir) != helperDir || !pathWithin(helperDir, jobPath) || !pathWithin(helperDir, job.StagingRoot) || !pathWithin(job.StagingRoot, job.Staged) {
		return fmt.Errorf("invalid staging path")
	}
	if err := validateRecordedJob(job, job.StateDir); err != nil {
		return err
	}
	return nil
}

func validateRecordedJob(job helperJob, stateDir string) error {
	if filepath.Clean(job.StateDir) != filepath.Clean(stateDir) || !filepath.IsAbs(job.Target) || !filepath.IsAbs(job.Staged) || !filepath.IsAbs(job.Backup) || !filepath.IsAbs(job.WorkDir) || !pathWithin(job.WorkDir, job.StagingRoot) || !pathWithin(job.StagingRoot, job.Staged) {
		return fmt.Errorf("invalid helper paths")
	}
	recorded, err := os.ReadFile(filepath.Join(job.StateDir, "updater-install-target"))
	if err != nil || filepath.Clean(string(recorded)) != filepath.Clean(job.Target) {
		return fmt.Errorf("install target does not match record")
	}
	if filepath.Dir(job.Backup) != filepath.Dir(job.Target) || !strings.HasPrefix(filepath.Base(job.Backup), "."+filepath.Base(job.Target)+".nex-backup-") {
		return fmt.Errorf("invalid backup path")
	}
	if job.ParentPID <= 0 || job.Target == "" || job.StateDir == "" || job.AppID == "" {
		return fmt.Errorf("invalid helper job")
	}
	if job.LaunchEntry != "" {
		entry, err := safeArchivePath(job.LaunchEntry)
		if err != nil || entry != job.LaunchEntry {
			return fmt.Errorf("invalid launch entry")
		}
	}
	return nil
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func rollbackSwapped(job helperJob, journalPath, reason string) error {
	os.RemoveAll(job.Target)
	if err := os.Rename(job.Backup, job.Target); err != nil {
		return err
	}
	return finishRollback(job, journalPath, reason, true)
}

func recordRejected(job helperJob) {
	if job.Version == "" {
		return
	}
	_ = writeJSONAtomic(filepath.Join(job.StateDir, rejectedFile), rejectedRelease{Version: job.Version, Build: job.Build})
}

func finishRollback(job helperJob, journalPath, reason string, relaunch bool) error {
	recordRejected(job)
	err := writeResult(job.StateDir, Result{Status: "rolledBack:" + reason, Reason: reason, Time: time.Now().UTC()})
	if relaunch {
		_, _ = startDetached(launchPath(job.Target, job.LaunchEntry), withoutEnvironment(os.Environ(), healthEnvironment, helperJobEnvironment))
	}
	os.Remove(journalPath)
	os.RemoveAll(job.StagingRoot)
	cleanupHelper(job)
	return err
}

func cleanupHelper(job helperJob) {
	self, err := os.Executable()
	if err == nil && strings.HasPrefix(filepath.Base(self), "helper-") && pathWithin(job.WorkDir, self) {
		os.Remove(self)
	}
	sweepWorkDir(job.WorkDir)
	os.Remove(job.WorkDir)
}

func sweepWorkDir(workDir string) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "helper-") || strings.HasPrefix(name, "job-") {
			os.Remove(filepath.Join(workDir, name))
		}
	}
}

func launchPath(target, entry string) string {
	if entry == "" {
		return target
	}
	return filepath.Join(target, entry)
}

func writeResult(stateDir string, result Result) error {
	return writeJSONAtomic(filepath.Join(stateDir, "updater-result.json"), result)
}

func replaceEnvironment(environment []string, key, value string) []string {
	result := make([]string, 0, len(environment)+1)
	prefix := key + "="
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	if value != "" {
		result = append(result, prefix+value)
	}
	return result
}

func withoutEnvironment(environment []string, keys ...string) []string {
	result := environment
	for _, key := range keys {
		result = replaceEnvironment(result, key, "")
	}
	return result
}

func executableSuffix() string {
	if filepath.Ext(os.Args[0]) == ".exe" {
		return ".exe"
	}
	return ""
}

func Recover(stateDir string) error {
	if os.Getenv(healthEnvironment) != "" {
		return nil
	}
	journalPath := filepath.Join(stateDir, "updater-journal.json")
	data, err := os.ReadFile(journalPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return coded("state_unavailable", err)
	}
	var record journal
	if err := json.Unmarshal(data, &record); err != nil {
		return coded("state_unavailable", err)
	}
	job := record.Job
	if err := validateRecordedJob(job, stateDir); err != nil {
		return coded("rollback_failed", err)
	}
	if backupInfo, backupErr := os.Stat(job.Backup); backupErr == nil && backupInfo != nil {
		if _, targetErr := os.Lstat(job.Target); targetErr == nil {
			if err := os.RemoveAll(job.Target); err != nil {
				return coded("rollback_failed", err)
			}
		}
		if err := os.Rename(job.Backup, job.Target); err != nil {
			return coded("rollback_failed", err)
		}
		_, _ = startDetached(launchPath(job.Target, job.LaunchEntry), withoutEnvironment(os.Environ(), healthEnvironment, helperJobEnvironment))
	}
	recordRejected(job)
	if err := writeResult(stateDir, Result{Status: "rolledBack:interrupted", Reason: "interrupted", Time: time.Now().UTC()}); err != nil {
		return coded("state_unavailable", err)
	}
	os.RemoveAll(job.StagingRoot)
	os.Remove(journalPath)
	return nil
}
