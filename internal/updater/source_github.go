package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type githubAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt time.Time     `json:"published_at"`
	Body        string        `json:"body"`
	Assets      []githubAsset `json:"assets"`
}

func (u *Updater) checkGitHub(ctx context.Context) (*Release, error) {
	owner, repo, err := githubRepo(u.cfg.SourceURL)
	if err != nil {
		return nil, coded("source_unreachable", err)
	}
	cacheKey := sha256.Sum256([]byte(owner + "/" + repo))
	base := "github-" + hex.EncodeToString(cacheKey[:8])
	stateDir := u.host.StateDir()
	if err := ensurePrivateDir(stateDir); err != nil {
		return nil, coded("state_unavailable", err)
	}
	etagPath := filepath.Join(stateDir, base+".etag")
	cachePath := filepath.Join(stateDir, base+".json")
	headers := make(http.Header)
	if value, readErr := os.ReadFile(etagPath); readErr == nil {
		headers.Set("If-None-Match", strings.TrimSpace(string(value)))
	}
	apiURL := "https://api.github.com/repos/" + owner + "/" + repo + "/releases"
	response, err := u.request(ctx, http.MethodGet, apiURL, headers, true)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var data []byte
	if response.StatusCode == http.StatusNotModified {
		data, err = os.ReadFile(cachePath)
		if err != nil {
			return nil, coded("source_unreachable", fmt.Errorf("304 without cached metadata"))
		}
	} else {
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			return nil, coded("rate_limited", fmt.Errorf("GitHub returned %s", response.Status))
		}
		if response.StatusCode != http.StatusOK {
			return nil, coded("source_unreachable", fmt.Errorf("GitHub returned %s", response.Status))
		}
		data, err = readLimited(response.Body, metadataLimit)
		if err != nil {
			return nil, coded("source_unreachable", err)
		}
		if err := os.WriteFile(cachePath, data, 0600); err != nil {
			return nil, coded("state_unavailable", err)
		}
		if etag := response.Header.Get("ETag"); etag != "" {
			if err := os.WriteFile(etagPath, []byte(etag), 0600); err != nil {
				return nil, coded("state_unavailable", err)
			}
		}
	}
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, coded("source_unreachable", err)
	}
	assetName := strings.ReplaceAll(strings.ReplaceAll(u.cfg.AssetPattern, "{os}", runtime.GOOS), "{arch}", runtime.GOARCH)
	var selected *githubRelease
	selectedVersion := ""
	selectedBuild := ""
	for _, item := range releases {
		if item.Draft || item.Prerelease && !u.state.Settings.IncludePrerelease {
			continue
		}
		version, build, ok := ParseTag(item.TagName)
		if !ok {
			continue
		}
		comparison := Compare(version, build, selectedVersion, selectedBuild)
		if selected == nil || comparison > 0 || comparison == 0 && item.PublishedAt.After(selected.PublishedAt) {
			copy := item
			selected = &copy
			selectedVersion = version
			selectedBuild = build
		}
	}
	if selected == nil {
		return nil, nil
	}
	var asset *githubAsset
	var signatureAsset *githubAsset
	for i := range selected.Assets {
		if selected.Assets[i].Name == assetName {
			asset = &selected.Assets[i]
		}
		if selected.Assets[i].Name == assetName+".sig" {
			signatureAsset = &selected.Assets[i]
		}
	}
	if asset == nil {
		return nil, nil
	}
	date, _ := parseBuild(selectedBuild)
	release := &Release{Version: selectedVersion, Build: selectedBuild, Date: date.Format("2006-01-02"), Notes: selected.Body, PublishedAt: selected.PublishedAt}
	release.Asset = Asset{URL: asset.BrowserDownloadURL, Size: asset.Size, Layout: u.layout()}
	release.Asset.SHA256, release.Installable = parseDigest(asset.Digest)
	if !release.Installable {
		release.Reason = "no_hash"
	}
	if len(u.cfg.PublicKeys) > 0 {
		if !release.Installable || signatureAsset == nil {
			return nil, coded("signature_invalid", fmt.Errorf("signature is missing"))
		}
		signature, fetchErr := u.fetchSignature(ctx, signatureAsset.BrowserDownloadURL)
		if fetchErr != nil || !verifySignature(release.Asset.SHA256, signature, u.cfg.PublicKeys) {
			return nil, coded("signature_invalid", fmt.Errorf("signature is invalid"))
		}
		release.Asset.Signature = signature
	}
	return release, nil
}

func parseDigest(value string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(value), "sha256:") {
		return "", false
	}
	digest := strings.ToLower(strings.TrimSpace(value[len("sha256:"):]))
	decoded, err := hex.DecodeString(digest)
	return digest, err == nil && len(decoded) == sha256.Size
}

func (u *Updater) fetchSignature(ctx context.Context, rawURL string) ([]byte, error) {
	response, err := u.request(ctx, http.MethodGet, rawURL, nil, true)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, coded("signature_invalid", fmt.Errorf("signature returned %s", response.Status))
	}
	data, err := readLimited(response.Body, 16<<10)
	if err != nil {
		return nil, coded("signature_invalid", err)
	}
	return decodeSignature(data)
}
