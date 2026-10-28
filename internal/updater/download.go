package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (u *Updater) Download(ctx context.Context) error {
	if u.host == nil {
		return coded("not_configured", nil)
	}
	if !u.begin() {
		return coded("busy", nil)
	}
	defer u.finish()
	if err := u.host.Authorize("download"); err != nil {
		return coded("unauthorized", err)
	}
	u.mu.RLock()
	release := u.release
	u.mu.RUnlock()
	if release == nil {
		return coded("no_update", nil)
	}
	if !release.Installable || release.Asset.SHA256 == "" {
		return coded("no_hash", nil)
	}
	if u.host.InstallMode() != InstallSelf {
		return coded("not_applicable", fmt.Errorf("host is notify-only"))
	}
	if release.Asset.Size < 0 || release.Asset.Size > downloadLimit {
		return coded("not_applicable", fmt.Errorf("declared size is invalid"))
	}
	target, err := installTarget()
	if err != nil {
		return coded("not_applicable", err)
	}
	workDir := filepath.Join(filepath.Dir(target), ".nex-updater-"+safeName(u.cfg.AppID))
	if err := ensurePrivateDir(workDir); err != nil {
		return coded("not_applicable", err)
	}
	sweepWorkDir(workDir)
	stageDir, err := os.MkdirTemp(workDir, "stage-")
	if err != nil {
		return coded("not_applicable", err)
	}
	if err := os.Chmod(stageDir, 0700); err != nil {
		os.RemoveAll(stageDir)
		return coded("not_applicable", err)
	}
	archivePath := filepath.Join(stageDir, "update.zip")
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(stageDir)
		}
	}()
	u.setStatus("downloading", nil)
	response, err := u.request(ctx, http.MethodGet, release.Asset.URL, nil, false)
	if err != nil {
		u.setStatus("error", err)
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		err := coded("source_unreachable", fmt.Errorf("asset returned %s", response.Status))
		u.setStatus("error", err)
		return err
	}
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	hash := sha256.New()
	writer := io.MultiWriter(file, hash)
	buffer := make([]byte, 64<<10)
	var received int64
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			err = coded("cancelled", err)
			u.setStatus("error", err)
			return err
		}
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			received += int64(count)
			if received > downloadLimit || release.Asset.Size > 0 && received > release.Asset.Size {
				file.Close()
				err = coded("size_mismatch", fmt.Errorf("download exceeds declared size"))
				u.setStatus("error", err)
				return err
			}
			if _, err := writer.Write(buffer[:count]); err != nil {
				file.Close()
				err = coded("not_applicable", err)
				u.setStatus("error", err)
				return err
			}
			u.mu.Lock()
			u.state.Progress = Progress{Received: received, Total: release.Asset.Size}
			u.mu.Unlock()
			u.host.Emit("updater.progress", Progress{Received: received, Total: release.Asset.Size})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			file.Close()
			err = coded("source_unreachable", readErr)
			u.setStatus("error", err)
			return err
		}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	if err := file.Close(); err != nil {
		err = coded("not_applicable", err)
		u.setStatus("error", err)
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, normalizeHash(release.Asset.SHA256)) {
		err = coded("checksum_mismatch", fmt.Errorf("downloaded asset hash does not match"))
		u.setStatus("error", err)
		return err
	}
	if len(u.cfg.PublicKeys) > 0 && !verifySignature(actual, release.Asset.Signature, u.cfg.PublicKeys) {
		err = coded("signature_invalid", fmt.Errorf("asset signature does not verify"))
		u.setStatus("error", err)
		return err
	}
	u.mu.Lock()
	u.stageDir = stageDir
	u.archivePath = archivePath
	u.stagedPath = ""
	u.launchEntry = ""
	u.state.Progress = Progress{Received: received, Total: release.Asset.Size}
	u.mu.Unlock()
	cleanup = false
	u.setStatus("downloaded", nil)
	return nil
}

func safeName(value string) string {
	var result strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	if result.Len() == 0 {
		return "app"
	}
	return result.String()
}
