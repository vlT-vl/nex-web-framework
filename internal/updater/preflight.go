package updater

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func preflight(ctx context.Context, target, staged, launchEntry string, requiredBytes int64) error {
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("install target is unavailable: %w", err)
	}
	if info.IsDir() != mustBeDirectory(staged) {
		return fmt.Errorf("staged layout does not match target")
	}
	probe, err := os.CreateTemp(filepath.Dir(target), ".nex-write-probe-")
	if err != nil {
		return fmt.Errorf("install directory is not writable: %w", err)
	}
	probePath := probe.Name()
	probe.Close()
	os.Remove(probePath)
	if available, err := availableSpace(filepath.Dir(target)); err == nil && requiredBytes > 0 && available < uint64(requiredBytes) {
		return fmt.Errorf("not enough free space")
	}
	executable := staged
	if launchEntry != "" {
		executable = filepath.Join(staged, launchEntry)
	}
	if executableInfo, err := os.Stat(executable); err != nil || !executableInfo.Mode().IsRegular() {
		return fmt.Errorf("staged executable is unavailable")
	}
	if err := runSelfCheck(ctx, executable); err != nil {
		return err
	}
	return nil
}

func mustBeDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func runSelfCheck(ctx context.Context, executable string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(checkCtx, executable, selfCheckFlag)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if err == nil {
		return nil
	}
	text := strings.ToLower(output.String())
	if strings.Contains(text, "flag provided but not defined") || strings.Contains(text, "unknown flag") || strings.Contains(text, "unknown option") || strings.Contains(text, "unrecognized option") {
		return nil
	}
	if checkCtx.Err() != nil {
		return fmt.Errorf("staged executable self-check timed out")
	}
	return fmt.Errorf("staged executable self-check failed: %w", err)
}
