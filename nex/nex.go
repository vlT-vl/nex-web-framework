// Package nex is the public facade of the nex-web framework.
// Applications import only this package.
package nex

import (
	"nex-web/config"
	"nex-web/internal/app"
	"nex-web/internal/core"
	"nex-web/internal/meta"
)

// Public type aliases.
type (
	App              = app.App
	Config           = app.Config
	Context          = core.Context
	HandlerFunc      = core.HandlerFunc
	RPCError         = core.RPCError
	SecurityDecision = core.SecurityDecision
	SecurityPolicy   = core.SecurityPolicy
	Env              = config.Env
)

// Errorf creates a typed RPC error returned to the frontend.
var Errorf = core.Errorf

// New creates a new application.
func New(cfg Config) *App { return app.New(cfg) }

// Framework identity — single source of truth is internal/meta.
// Override at build time: -ldflags "-X nex-web/internal/meta.Version=x.y.z -X nex-web/internal/meta.Build=R..."
const Name = meta.Name

func FrameworkVersion() string { return meta.Version }
func FrameworkBuild() string   { return meta.Build }
func FrameworkUpdated() string { return meta.Updated }
func FrameworkAuthor() string  { return meta.Author }
