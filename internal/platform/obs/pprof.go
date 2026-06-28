// Package obs provides observability utilities for PerGo, including
// structured logging, pprof debug server, and expvar metrics.
package obs

import (
	"context"
	"expvar"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
)

// DebugServer wraps an HTTP server with pprof and expvar handlers,
// providing access to the listener address for test verification.
type DebugServer struct {
	*http.Server
	listener net.Listener
}

// Addr returns the listener address (useful when started on ":0").
func (ds *DebugServer) Addr() string {
	return ds.listener.Addr().String()
}

// StartDebugServer creates an HTTP server on the given address with pprof
// and expvar handlers, then starts it in a background goroutine.
// The returned DebugServer can be shut down gracefully via its Shutdown method.
// Returns an error (instead of panicking) if the address cannot be bound —
// the debug server is optional observability tooling and a port conflict
// must never crash the main application.
func StartDebugServer(addr string) (*DebugServer, error) {
	mux := http.NewServeMux()
	mux.Handle("/debug/pprof/", http.DefaultServeMux)
	mux.Handle("/debug/vars", expvar.Handler())

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("obs: failed to listen on %s: %w", addr, err)
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			// Server was closed unexpectedly; nothing to do in background goroutine.
			_ = err
		}
	}()

	return &DebugServer{
		Server:   srv,
		listener: ln,
	}, nil
}

// Shutdown gracefully shuts down the debug server.
func (ds *DebugServer) Shutdown(ctx context.Context) error {
	return ds.Server.Shutdown(ctx)
}

// RegisterCounter registers a new named expvar.Int counter and returns it
// for incrementing. The counter is immediately visible at /debug/vars.
func RegisterCounter(name string) *expvar.Int {
	return expvar.NewInt(name)
}
