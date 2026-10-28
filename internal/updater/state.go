package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Settings struct {
	AutoCheck         bool `json:"autoCheck"`
	AutoDownload      bool `json:"autoDownload"`
	IncludePrerelease bool `json:"includePrerelease"`
}

type UpdateInfo struct {
	Version     string `json:"version"`
	Build       string `json:"build"`
	Date        string `json:"date"`
	Notes       string `json:"notes"`
	Mandatory   bool   `json:"mandatory"`
	Installable bool   `json:"installable"`
	Reason      string `json:"reason"`
	RolledBack  bool   `json:"rolledBack"`
}

type Progress struct {
	Received int64 `json:"received"`
	Total    int64 `json:"total"`
}

type State struct {
	Configured     bool        `json:"configured"`
	Source         string      `json:"source"`
	CurrentVersion string      `json:"currentVersion"`
	CurrentBuild   string      `json:"currentBuild"`
	Status         string      `json:"status"`
	Update         *UpdateInfo `json:"update"`
	Progress       Progress    `json:"progress"`
	Settings       Settings    `json:"settings"`
	LastCheck      time.Time   `json:"lastCheck,omitempty"`
	Error          string      `json:"error"`
}

type Result struct {
	Status string    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	Time   time.Time `json:"time"`
}

type Updater struct {
	mu          sync.RWMutex
	cfg         Config
	host        Host
	state       State
	release     *Release
	stageDir    string
	archivePath string
	stagedPath  string
	launchEntry string
	busy        bool
}

func New(cfg Config, host Host) *Updater {
	cfg = normalizeConfig(cfg, host)
	version, build := "", ""
	if host != nil {
		version, build = host.Version()
	}
	u := &Updater{cfg: cfg, host: host}
	u.state = State{Configured: cfg.SourceURL != "", CurrentVersion: version, CurrentBuild: build, Status: "idle"}
	if cfg.SourceURL != "" {
		if isGitHubSource(cfg.SourceURL) {
			u.state.Source = "github"
		} else {
			u.state.Source = "feed"
		}
	}
	u.state.Settings = u.loadSettings()
	return u
}

func (u *Updater) begin() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.busy {
		return false
	}
	u.busy = true
	return true
}

func (u *Updater) finish() {
	u.mu.Lock()
	u.busy = false
	u.mu.Unlock()
}

func (u *Updater) SetSourceIfUnset(rawURL string) {
	if rawURL == "" || !u.begin() {
		return
	}
	defer u.finish()
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cfg.SourceURL != "" {
		return
	}
	u.cfg.SourceURL = rawURL
	u.state.Configured = true
	if isGitHubSource(rawURL) {
		u.state.Source = "github"
	} else {
		u.state.Source = "feed"
	}
}

func (u *Updater) State() State {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return cloneState(u.state)
}

func cloneState(state State) State {
	if state.Update != nil {
		update := *state.Update
		state.Update = &update
	}
	return state
}

func (u *Updater) setStatus(status string, err error) {
	u.mu.Lock()
	u.state.Status = status
	if err == nil {
		u.state.Error = ""
	} else if code := ErrorCode(err); code != "" {
		u.state.Error = code
	} else {
		u.state.Error = err.Error()
	}
	state := cloneState(u.state)
	u.mu.Unlock()
	if u.host != nil {
		u.host.Emit("updater.state", state)
	}
}

func (u *Updater) SetSettings(settings Settings) error {
	if u.host == nil {
		return coded("not_configured", nil)
	}
	if err := ensurePrivateDir(u.host.StateDir()); err != nil {
		return coded("state_unavailable", err)
	}
	if err := writeJSONAtomic(filepath.Join(u.host.StateDir(), "updater-settings.json"), settings); err != nil {
		return coded("state_unavailable", err)
	}
	u.mu.Lock()
	u.state.Settings = settings
	state := cloneState(u.state)
	u.mu.Unlock()
	u.host.Emit("updater.state", state)
	return nil
}

func (u *Updater) loadSettings() Settings {
	defaults := Settings{AutoCheck: true}
	if u.host == nil || u.host.StateDir() == "" {
		return defaults
	}
	data, err := os.ReadFile(filepath.Join(u.host.StateDir(), "updater-settings.json"))
	if err != nil {
		return defaults
	}
	var settings Settings
	if json.Unmarshal(data, &settings) != nil {
		return defaults
	}
	return settings
}

func (u *Updater) LastResult() (*Result, error) {
	if u.host == nil {
		return nil, nil
	}
	path := filepath.Join(u.host.StateDir(), "updater-result.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, coded("state_unavailable", err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, coded("state_unavailable", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, coded("state_unavailable", err)
	}
	return &result, nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
