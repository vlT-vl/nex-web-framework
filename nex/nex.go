package nex

import (
	"nex-web/config"
	"nex-web/internal/app"
	"nex-web/internal/core"
	"nex-web/internal/meta"
)

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

var Errorf = core.Errorf

func New(cfg Config) *App { return app.New(cfg) }

const Name = meta.Name

func FrameworkVersion() string { return meta.Version }
func FrameworkBuild() string   { return meta.Build }
func FrameworkUpdated() string { return meta.Updated }
func FrameworkAuthor() string  { return meta.Author }
