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

func main() {
	publicAddr := envDefault("NEXWEB_PUBLIC_ADDR", ":3000")
	daemon.MaybeDetach(nexweb.FrameworkVersion(), publicAddr)

	envFile := envDefault("NEXWEB_ENV_FILE", ".env")
	config.Load(envFile)

	log.Printf("%s %s (%s)", nexweb.Name, nexweb.FrameworkVersion(), nexweb.FrameworkBuild())

	a := nexweb.New(nexweb.Config{
		// App identity — read from .env so no recompile is needed to change them.
		Name:    envDefault("NEXWEB_APP_NAME", "nex-web-template"),
		Version: envDefault("NEXWEB_APP_VERSION", "dev"),
		Build:   envDefault("NEXWEB_APP_BUILD", ""),
		Author:  envDefault("NEXWEB_APP_AUTHOR", ""),
		// Framework + infrastructure config.
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

// envDefault returns the env variable value if set (even to ""),
// otherwise returns fallback. Setting a var to "" explicitly disables features.
func envDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
