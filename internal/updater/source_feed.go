package updater

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

type feedAsset struct {
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Sig    string `json:"sig"`
	Layout string `json:"layout"`
	Entry  string `json:"entry"`
}

type feedDocument struct {
	App       string               `json:"app"`
	Version   string               `json:"version"`
	Build     string               `json:"build"`
	Date      string               `json:"date"`
	Notes     string               `json:"notes"`
	Mandatory bool                 `json:"mandatory"`
	URL       string               `json:"url"`
	Assets    map[string]feedAsset `json:"assets"`
}

func (u *Updater) checkFeed(ctx context.Context) (*Release, error) {
	response, err := u.request(ctx, http.MethodGet, u.cfg.SourceURL, nil, true)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		return nil, coded("rate_limited", fmt.Errorf("feed returned %s", response.Status))
	}
	if response.StatusCode != http.StatusOK {
		return nil, coded("source_unreachable", fmt.Errorf("feed returned %s", response.Status))
	}
	data, err := readLimited(response.Body, metadataLimit)
	if err != nil {
		return nil, coded("source_unreachable", err)
	}
	var document feedDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, coded("source_unreachable", err)
	}
	if document.Version == "" {
		return nil, coded("source_unreachable", fmt.Errorf("feed version is missing"))
	}
	if len(document.Assets) == 0 {
		return &Release{Version: document.Version, Build: document.Build, Date: document.Date, Notes: document.Notes, Mandatory: document.Mandatory, Installable: false, Reason: "legacy_feed", Asset: Asset{URL: document.URL}}, nil
	}
	if document.App != u.cfg.AppID {
		return nil, coded("wrong_app", fmt.Errorf("feed app does not match"))
	}
	item, ok := document.Assets[runtime.GOOS+"-"+runtime.GOARCH]
	if !ok {
		return nil, nil
	}
	release := &Release{Version: document.Version, Build: document.Build, Date: document.Date, Notes: document.Notes, Mandatory: document.Mandatory}
	release.Asset = Asset{URL: item.URL, Size: item.Size, SHA256: normalizeHash(item.SHA256), Layout: item.Layout, Entry: item.Entry}
	if release.Asset.Layout == "" || u.cfg.Layout != "" {
		release.Asset.Layout = u.layout()
	}
	release.Installable = validHash(release.Asset.SHA256)
	if !release.Installable {
		release.Reason = "no_hash"
	}
	if len(u.cfg.PublicKeys) > 0 {
		if !release.Installable || item.Sig == "" {
			return nil, coded("signature_invalid", fmt.Errorf("signature is missing"))
		}
		signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(item.Sig))
		if err != nil || !verifySignature(release.Asset.SHA256, signature, u.cfg.PublicKeys) {
			return nil, coded("signature_invalid", fmt.Errorf("signature is invalid"))
		}
		release.Asset.Signature = signature
	}
	return release, nil
}

func normalizeHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.TrimPrefix(value, "sha256:")
}

func validHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
