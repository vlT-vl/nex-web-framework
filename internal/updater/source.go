package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (u *Updater) Check(ctx context.Context, manual bool) (State, error) {
	_ = manual
	if u.host == nil || u.cfg.SourceURL == "" {
		err := coded("not_configured", nil)
		u.setStatus("error", err)
		return u.State(), err
	}
	if !u.begin() {
		return u.State(), nil
	}
	defer u.finish()
	if err := u.host.Authorize("check"); err != nil {
		err = coded("unauthorized", err)
		u.setStatus("error", err)
		return u.State(), err
	}
	u.setStatus("checking", nil)
	var release *Release
	var err error
	if isGitHubSource(u.cfg.SourceURL) {
		release, err = u.checkGitHub(ctx)
	} else {
		release, err = u.checkFeed(ctx)
	}
	now := time.Now().UTC()
	u.mu.Lock()
	u.state.LastCheck = now
	u.mu.Unlock()
	if err != nil {
		u.setStatus("error", err)
		return u.State(), err
	}
	version, build := u.host.Version()
	if release == nil || Compare(release.Version, release.Build, version, build) <= 0 {
		u.mu.Lock()
		u.release = nil
		u.state.Update = nil
		u.state.Progress = Progress{}
		u.mu.Unlock()
		u.setStatus("idle", nil)
		return u.State(), nil
	}
	u.mu.Lock()
	u.release = release
	u.state.Update = &UpdateInfo{Version: release.Version, Build: release.Build, Date: release.Date, Notes: release.Notes, Mandatory: release.Mandatory, Installable: release.Installable, Reason: release.Reason, RolledBack: u.wasRolledBack(release)}
	u.state.Progress = Progress{Total: release.Asset.Size}
	u.mu.Unlock()
	u.setStatus("available", nil)
	return u.State(), nil
}

func (u *Updater) wasRolledBack(release *Release) bool {
	data, err := os.ReadFile(filepath.Join(u.host.StateDir(), rejectedFile))
	if err != nil {
		return false
	}
	var rejected rejectedRelease
	if json.Unmarshal(data, &rejected) != nil {
		return false
	}
	return rejected.Version == release.Version && rejected.Build == release.Build
}

func isGitHubSource(rawURL string) bool {
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme != "https" || !strings.EqualFold(target.Hostname(), "github.com") {
		return false
	}
	parts := splitPath(target.Path)
	if len(parts) == 3 && parts[2] == "releases" {
		parts = parts[:2]
	}
	if len(parts) != 2 || parts[0] == "" || strings.TrimSuffix(parts[1], ".git") == "" {
		return false
	}
	return target.RawQuery == "" && target.Fragment == ""
}

func githubRepo(rawURL string) (string, string, error) {
	if !isGitHubSource(rawURL) {
		return "", "", fmt.Errorf("invalid GitHub repository URL")
	}
	target, _ := url.Parse(rawURL)
	parts := splitPath(target.Path)
	if len(parts) == 3 {
		parts = parts[:2]
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
