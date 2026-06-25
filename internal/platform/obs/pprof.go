// Package obs provides observability utilities for OmniGo, including
// structured logging, pprof debug server, and expvar metrics.
package obs

import (
	"expvar"
	"net"
	"net/http"
	_ "net/http/pprof"
)

// DebugServer wraps an HTTP server with pprof and expvar handlers.
type DebugServer struct {
	*http.Server
	listener net.Listener
}

// Addr returns the listener address.
func (ds *DebugServer) Addr() string {
	return ds.listener.Addr().String()
}

// StartDebugServer creates an HTTP server on the given address with pprof
// and expvar handlers, then starts it in a background goroutine.
// TODO: implement — currently a stub for RED phase.
func StartDebugServer(addr string) *DebugServer {
	panic("obs: StartDebugServer not implemented")
}

// RegisterCounter registers a new named expvar.Int counter and returns it.
// TODO: implement — currently a stub for RED phase.
func RegisterCounter(name string) *expvar.Int {
	panic("obs: RegisterCounter not implemented")
}
