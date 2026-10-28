package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func (u *Updater) Install(ctx context.Context) error {
	if u.host == nil {
		return coded("not_configured", nil)
	}
	if !u.begin() {
		return coded("busy", nil)
	}
	handedOver := false
	defer func() {
		if !handedOver {
			u.finish()
		}
	}()
	if err := u.host.Authorize("install"); err != nil {
		return coded("unauthorized", err)
	}
	if u.host.InstallMode() != InstallSelf {
		return coded("not_applicable", fmt.Errorf("host is notify-only"))
	}
	u.mu.RLock()
	release := u.release
	stageDir := u.stageDir
	archivePath := u.archivePath
	u.mu.RUnlock()
	if release == nil || stageDir == "" || archivePath == "" {
		return coded("not_applicable", fmt.Errorf("no verified download is staged"))
	}
	if !release.Installable {
		return coded("no_hash", nil)
	}
	extracted := filepath.Join(stageDir, "extracted")
	os.RemoveAll(extracted)
	if err := extractArchive(archivePath, extracted); err != nil {
		u.setStatus("error", err)
		return err
	}
	staged, launchEntry, err := validateLayout(extracted, release.Asset)
	if err != nil {
		err = coded("bad_archive", err)
		u.setStatus("error", err)
		return err
	}
	target, err := installTarget()
	if err != nil {
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	required, _ := treeSize(target)
	stagedSize, _ := treeSize(staged)
	required += stagedSize
	if err := preflight(ctx, target, staged, launchEntry, required); err != nil {
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	workDir := filepath.Dir(stageDir)
	identifier := strconv.FormatInt(time.Now().UnixNano(), 36)
	backup := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".nex-backup-"+identifier)
	job := helperJob{AppID: u.cfg.AppID, Version: release.Version, Build: release.Build, StateDir: u.host.StateDir(), WorkDir: workDir, StagingRoot: stageDir, Staged: staged, Target: target, Backup: backup, LaunchEntry: launchEntry, ParentPID: os.Getpid(), ParentWait: 30 * time.Second, HealthTimeout: u.cfg.HealthTimeout}
	if err := ensurePrivateDir(job.StateDir); err != nil {
		err = coded("state_unavailable", err)
		u.setStatus("error", err)
		return err
	}
	if err := os.WriteFile(filepath.Join(job.StateDir, "updater-install-target"), []byte(target), 0600); err != nil {
		err = coded("state_unavailable", err)
		u.setStatus("error", err)
		return err
	}
	jobPath := filepath.Join(workDir, "job-"+identifier+".json")
	if err := writeJSONAtomic(jobPath, job); err != nil {
		err = coded("state_unavailable", err)
		u.setStatus("error", err)
		return err
	}
	currentExecutable, err := os.Executable()
	if err != nil {
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	helperPath := filepath.Join(workDir, "helper-"+identifier+executableSuffix())
	if err := copyExecutable(currentExecutable, helperPath); err != nil {
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	environment := replaceEnvironment(u.host.RelaunchEnv(), helperJobEnvironment, jobPath)
	command, err := startDetached(helperPath, environment)
	if err != nil {
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	u.mu.Lock()
	u.stagedPath = staged
	u.launchEntry = launchEntry
	u.mu.Unlock()
	u.setStatus("installing", nil)
	if err := u.host.Quiesce(); err != nil {
		_ = command.Process.Kill()
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	handedOver = true
	return nil
}

func treeSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()|0500)
	if err != nil {
		return err
	}
	if _, err := output.ReadFrom(input); err != nil {
		output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}
