package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type fakeHost struct {
	appID   string
	version string
	build   string
	state   string
	mode    InstallMode
	mu      sync.Mutex
	events  []string
}

func (h *fakeHost) AppID() string             { return h.appID }
func (h *fakeHost) Version() (string, string) { return h.version, h.build }
func (h *fakeHost) StateDir() string          { return h.state }
func (h *fakeHost) Authorize(string) error    { return nil }
func (h *fakeHost) Quiesce() error            { return nil }
func (h *fakeHost) RelaunchEnv() []string     { return os.Environ() }
func (h *fakeHost) HealthMode() HealthMode    { return HealthBackend }
func (h *fakeHost) InstallMode() InstallMode  { return h.mode }
func (h *fakeHost) Emit(event string, _ any) {
	h.mu.Lock()
	h.events = append(h.events, event)
	h.mu.Unlock()
}

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	return t.base.RoundTrip(clone)
}

type localTLSServer struct {
	URL       string
	client    *http.Client
	closeFunc func()
}

func (s *localTLSServer) Client() *http.Client {
	return s.client
}

func (s *localTLSServer) Close() {
	s.closeFunc()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newTLSServer(t *testing.T, handler http.Handler) (server *localTLSServer) {
	t.Helper()
	defer func() {
		if recover() != nil {
			server = newMemoryTLSServer(handler)
		}
	}()
	real := httptest.NewTLSServer(handler)
	return &localTLSServer{URL: real.URL, client: real.Client(), closeFunc: real.Close}
}

func newMemoryTLSServer(handler http.Handler) *localTLSServer {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})
	return &localTLSServer{URL: "https://updater.test", client: &http.Client{Transport: transport}, closeFunc: func() {}}
}

func TestGitHubReleaseSelectionAndETag(t *testing.T) {
	assetName := strings.ReplaceAll(strings.ReplaceAll("build-{os}-{arch}.zip", "{os}", runtime.GOOS), "{arch}", runtime.GOARCH)
	digest := strings.Repeat("a", 64)
	requests := 0
	seenETag := false
	server := newTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/owner/repo/releases" {
			http.NotFound(writer, request)
			return
		}
		requests++
		if request.Header.Get("If-None-Match") == `"etag-1"` {
			seenETag = true
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("ETag", `"etag-1"`)
		items := []map[string]any{
			{"tag_name": "v9.0.0-R010126", "draft": true, "published_at": "2026-01-01T00:00:00Z", "assets": []any{}},
			{"tag_name": "v8.0.0-R010126", "prerelease": true, "published_at": "2026-01-01T00:00:00Z", "assets": []any{}},
			{"tag_name": "v0.2.0-R020226", "body": "new duplicate", "published_at": "2026-02-04T00:00:00Z", "assets": []map[string]any{{"name": assetName, "size": 12, "digest": "sha256:" + digest, "browser_download_url": "https://objects.githubusercontent.com/update.zip"}}},
			{"tag_name": "v0.2.0-R020226", "body": "old duplicate", "published_at": "2026-02-03T00:00:00Z", "assets": []map[string]any{{"name": assetName, "size": 12, "digest": "sha256:" + digest, "browser_download_url": "https://objects.githubusercontent.com/update.zip"}}},
			{"tag_name": "v0.3.0-bad", "published_at": "2026-04-01T00:00:00Z", "assets": []any{}},
			{"tag_name": "v0.1.9-R311226", "published_at": "2026-12-31T00:00:00Z", "assets": []map[string]any{{"name": assetName, "size": 12, "digest": "sha256:" + digest, "browser_download_url": "https://objects.githubusercontent.com/update.zip"}}},
		}
		json.NewEncoder(writer).Encode(items)
	}))
	defer server.Close()
	serverURL, _ := url.Parse(server.URL)
	client := server.Client()
	client.Transport = rewriteTransport{target: serverURL, base: client.Transport}
	host := &fakeHost{appID: "test-app", version: "0.1.0", build: "R010126", state: t.TempDir(), mode: InstallSelf}
	updater := New(Config{SourceURL: "https://github.com/owner/repo/releases/", HTTPClient: client}, host)
	state, err := updater.Check(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Update == nil || state.Update.Version != "0.2.0" || state.Update.Build != "R020226" || state.Update.Notes != "new duplicate" || !state.Update.Installable {
		t.Fatalf("unexpected selected update: %#v", state.Update)
	}
	state, err = updater.Check(context.Background(), true)
	if err != nil || state.Update == nil {
		t.Fatalf("cached check failed: %#v, %v", state.Update, err)
	}
	if requests != 2 || !seenETag {
		t.Fatalf("ETag flow requests=%d seen=%v", requests, seenETag)
	}
}

func TestGitHubAssetFailures(t *testing.T) {
	assetName := strings.ReplaceAll(strings.ReplaceAll("build-{os}-{arch}.zip", "{os}", runtime.GOOS), "{arch}", runtime.GOARCH)
	tests := []struct {
		name        string
		assets      []map[string]any
		code        string
		installable bool
		reason      string
		noUpdate    bool
	}{
		{name: "missing platform asset", assets: []map[string]any{{"name": "other.zip", "size": 1, "digest": "sha256:" + strings.Repeat("a", 64), "browser_download_url": "https://objects.githubusercontent.com/other.zip"}}, noUpdate: true},
		{name: "missing digest notify only", assets: []map[string]any{{"name": assetName, "size": 1, "browser_download_url": "https://objects.githubusercontent.com/update.zip"}}, reason: "no_hash"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				json.NewEncoder(writer).Encode([]map[string]any{{"tag_name": "v1.0.0-R010126", "published_at": "2026-01-01T00:00:00Z", "assets": test.assets}})
			}))
			defer server.Close()
			serverURL, _ := url.Parse(server.URL)
			client := server.Client()
			client.Transport = rewriteTransport{target: serverURL, base: client.Transport}
			host := &fakeHost{appID: "test-app", version: "0.1.0", build: "R010126", state: t.TempDir(), mode: InstallSelf}
			state, err := New(Config{SourceURL: "https://github.com/owner/repo", HTTPClient: client}, host).Check(context.Background(), true)
			if test.noUpdate {
				if err != nil || state.Update != nil || state.Status != "idle" {
					t.Fatalf("expected no update without error, got err=%v state=%#v", err, state)
				}
				return
			}
			if ErrorCode(err) != test.code {
				if test.code != "" || err != nil {
					t.Fatalf("error=%v code=%q", err, ErrorCode(err))
				}
			}
			if test.code == "" && (state.Update == nil || state.Update.Installable != test.installable || state.Update.Reason != test.reason) {
				t.Fatalf("unexpected update: %#v", state.Update)
			}
		})
	}
}

func TestFeedParsing(t *testing.T) {
	key := runtime.GOOS + "-" + runtime.GOARCH
	tests := []struct {
		name     string
		document map[string]any
		code     string
		reason   string
		noUpdate bool
	}{
		{name: "platform asset missing", document: map[string]any{"app": "test-app", "version": "1.0.0", "build": "R010126", "assets": map[string]any{"plan9-mips": map[string]any{"url": "https://example.com/a", "sha256": strings.Repeat("a", 64), "layout": "binary"}}}, noUpdate: true},
		{name: "wrong app", document: map[string]any{"app": "other", "version": "1.0.0", "build": "R010126", "assets": map[string]any{key: map[string]any{"url": "https://example.com/a", "sha256": strings.Repeat("a", 64), "layout": platformLayout()}}}, code: "wrong_app"},
		{name: "missing sha256", document: map[string]any{"app": "test-app", "version": "1.0.0", "build": "R010126", "assets": map[string]any{key: map[string]any{"url": "https://example.com/a", "layout": platformLayout()}}}, reason: "no_hash"},
		{name: "legacy", document: map[string]any{"version": "1.0.0", "url": "https://example.com/a", "notes": "legacy", "mandatory": true}, reason: "legacy_feed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				json.NewEncoder(writer).Encode(test.document)
			}))
			defer server.Close()
			target, _ := url.Parse(server.URL)
			host := &fakeHost{appID: "test-app", version: "0.1.0", build: "R010126", state: t.TempDir(), mode: InstallSelf}
			state, err := New(Config{SourceURL: server.URL, AllowedHosts: []string{target.Hostname()}, HTTPClient: server.Client()}, host).Check(context.Background(), true)
			if test.noUpdate {
				if err != nil || state.Update != nil || state.Status != "idle" {
					t.Fatalf("expected no update without error, got err=%v state=%#v", err, state)
				}
				return
			}
			if ErrorCode(err) != test.code {
				if test.code != "" || err != nil {
					t.Fatalf("error=%v code=%q", err, ErrorCode(err))
				}
			}
			if test.code == "" && (state.Update == nil || state.Update.Installable || state.Update.Reason != test.reason) {
				t.Fatalf("unexpected update: %#v", state.Update)
			}
		})
	}
}

func TestFeedSignatures(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("asset"))
	hash := hex.EncodeToString(digest[:])
	good := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, digest[:]))
	bad := base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	tests := []struct {
		name string
		keys [][]byte
		sig  string
		code string
	}{
		{name: "keys absent signature skipped"},
		{name: "good", keys: [][]byte{publicKey}, sig: good},
		{name: "bad", keys: [][]byte{publicKey}, sig: bad, code: "signature_invalid"},
		{name: "missing", keys: [][]byte{publicKey}, code: "signature_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				document := map[string]any{"app": "test-app", "version": "1.0.0", "build": "R010126", "assets": map[string]any{runtime.GOOS + "-" + runtime.GOARCH: map[string]any{"url": "https://example.com/asset", "size": 5, "sha256": hash, "sig": test.sig, "layout": platformLayout()}}}
				json.NewEncoder(writer).Encode(document)
			}))
			defer server.Close()
			target, _ := url.Parse(server.URL)
			host := &fakeHost{appID: "test-app", version: "0.1.0", build: "R010126", state: t.TempDir(), mode: InstallSelf}
			_, err := New(Config{SourceURL: server.URL, AllowedHosts: []string{target.Hostname()}, PublicKeys: test.keys, HTTPClient: server.Client()}, host).Check(context.Background(), true)
			if ErrorCode(err) != test.code {
				t.Fatalf("error=%v code=%q want=%q", err, ErrorCode(err), test.code)
			}
		})
	}
}

func TestDownloadChecksumMismatchDeletesStaging(t *testing.T) {
	asset := []byte("not the expected asset")
	appID := "checksum-" + strings.ReplaceAll(t.Name(), "/", "-")
	server := newTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/feed" {
			document := map[string]any{"app": appID, "version": "1.0.0", "build": "R010126", "assets": map[string]any{runtime.GOOS + "-" + runtime.GOARCH: map[string]any{"url": fmt.Sprintf("%s/asset", "https://"+request.Host), "size": len(asset), "sha256": strings.Repeat("0", 64), "layout": platformLayout()}}}
			json.NewEncoder(writer).Encode(document)
			return
		}
		writer.Write(asset)
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	host := &fakeHost{appID: appID, version: "0.1.0", build: "R010126", state: t.TempDir(), mode: InstallSelf}
	updater := New(Config{SourceURL: server.URL + "/feed", AllowedHosts: []string{target.Hostname()}, HTTPClient: server.Client()}, host)
	if _, err := updater.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := updater.Download(context.Background()); ErrorCode(err) != "checksum_mismatch" {
		t.Fatalf("Download() error=%v", err)
	}
	install, err := installTarget()
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(filepath.Dir(install), ".nex-updater-"+safeName(appID))
	defer os.RemoveAll(workDir)
	matches, err := filepath.Glob(filepath.Join(workDir, "stage-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging remains after mismatch: %v, %v", matches, err)
	}
}
