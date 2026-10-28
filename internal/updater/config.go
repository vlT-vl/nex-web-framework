package updater

import (
	"net/http"
	"time"
)

type Config struct {
	AppID         string
	SourceURL     string
	AssetPattern  string
	Layout        string
	AllowedHosts  []string
	PublicKeys    [][]byte
	HealthTimeout time.Duration
	HTTPClient    *http.Client
}

func normalizeConfig(cfg Config, host Host) Config {
	if cfg.AppID == "" && host != nil {
		cfg.AppID = host.AppID()
	}
	if cfg.AssetPattern == "" {
		cfg.AssetPattern = "build-{os}-{arch}.zip"
	}
	if len(cfg.AllowedHosts) == 0 {
		cfg.AllowedHosts = []string{"github.com", "api.github.com", "*.githubusercontent.com"}
	}
	if cfg.HealthTimeout <= 0 {
		cfg.HealthTimeout = 30 * time.Second
	}
	return cfg
}
