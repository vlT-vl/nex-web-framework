// Package core contains shared framework types: Context, HandlerFunc, Host,
// Registrar, RPCError. Kept separate to avoid circular imports.
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"nex-web/internal/session"
)

type HandlerFunc func(c *Context, params json.RawMessage) (any, error)

// SecurityDecision describes a sensitive operation before it is executed.
// Applications can inspect it through Config.SecurityPolicy or focused hooks.
type SecurityDecision struct {
	Method    string          `json:"method"`
	Category  string          `json:"category"`
	Operation string          `json:"operation"`
	Path      string          `json:"path,omitempty"`
	From      string          `json:"from,omitempty"`
	To        string          `json:"to,omitempty"`
	URL       string          `json:"url,omitempty"`
	Command   string          `json:"command,omitempty"`
	Key       string          `json:"key,omitempty"`
	Host      string          `json:"host,omitempty"`
	Port      int             `json:"port,omitempty"`
	Recursive bool            `json:"recursive,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// SecurityPolicy is optional. Returning nil allows the operation; returning an
// error denies it and the frontend receives an RPC error.
type SecurityPolicy interface {
	Allow(*Context, SecurityDecision) error
}

// Host is what the App exposes to handlers (event emission, process control).
// Web variant: no window management, no main-thread dispatch constraint.
type Host interface {
	Authorize(*Context, SecurityDecision) error
	OnMain(fn func()) // calls fn() directly; kept for interface parity with handlers
	Emit(event string, payload any)
	Quit()
}

// Registrar lets sub-packages register handlers without importing App.
type Registrar interface {
	Register(method string, fn HandlerFunc)
}

type Context struct {
	Ctx     context.Context
	Host    Host
	Session *session.Session
	Request *http.Request
	Method  string
}

func (c *Context) Authorize(d SecurityDecision) error {
	if d.Method == "" {
		d.Method = c.Method
	}
	if err := c.Host.Authorize(c, d); err != nil {
		if rpcErr, ok := err.(*RPCError); ok {
			return rpcErr
		}
		return Errorf("forbidden", "%v", err)
	}
	return nil
}

func (c *Context) OnMain(fn func())         { c.Host.OnMain(fn) }
func (c *Context) Emit(event string, p any) { c.Host.Emit(event, p) }
func (c *Context) Quit()                    { c.Host.Quit() }

func (c *Context) Bind(params json.RawMessage, v any) error {
	if len(params) == 0 {
		return nil
	}
	return json.Unmarshal(params, v)
}

type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return e.Message }

func Errorf(code, format string, a ...any) *RPCError {
	return &RPCError{Code: code, Message: fmt.Sprintf(format, a...)}
}
