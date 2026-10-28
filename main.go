package main

import (
	"embed"
	"encoding/json"
	"log"
	"os"
	"time"

	"nex-web/config"
	"nex-web/internal/daemon"
	nexweb "nex-web/nex"
)

//go:embed frontend/dist
var distFS embed.FS

var (
	buildName    string
	buildVersion string
	buildBuild   string
	buildAuthor  string
)

func main() {
	envFile := envDefault("NEXWEB_ENV_FILE", ".env")
	config.Load(envFile)

	appName := envDefault("NEXWEB_APP_NAME", baked(buildName, "nex-web-template"))
	appVersion := envDefault("NEXWEB_APP_VERSION", baked(buildVersion, "dev"))
	appBuild := envDefault("NEXWEB_APP_BUILD", baked(buildBuild, ""))

	publicAddr := envDefault("NEXWEB_PUBLIC_ADDR", ":3000")
	daemon.MaybeDetach(appName, appVersion, publicAddr)

	log.Printf("%s %s (%s) · %s %s (%s)", appName, appVersion, appBuild, nexweb.Name, nexweb.FrameworkVersion(), nexweb.FrameworkBuild())

	a := nexweb.New(nexweb.Config{
		Name:       appName,
		Version:    appVersion,
		Build:      appBuild,
		Author:     envDefault("NEXWEB_APP_AUTHOR", buildAuthor),
		Dist:       distFS,
		DistDir:    "frontend/dist",
		Addr:       envDefault("NEXWEB_ADDR", ":0"),
		PublicAddr: publicAddr,
		EnvFile:    envFile,
	})

	a.Handle("greet", func(c *nexweb.Context, params json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := c.Bind(params, &p); err != nil {
			return nil, nexweb.Errorf("bad_request", "%v", err)
		}
		if p.Name == "" {
			p.Name = "world"
		}
		return map[string]any{"message": "Hello, " + p.Name + "!"}, nil
	})

	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		n := 0
		for range t.C {
			n++
			a.Emit("tick", map[string]any{"n": n, "at": time.Now().Format(time.RFC3339)})
		}
	}()

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}

func envDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func baked(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
