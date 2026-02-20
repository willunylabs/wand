package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	listenAndServe = func(srv *http.Server) error {
		return srv.ListenAndServe()
	}
	shutdownServer = func(srv *http.Server, ctx context.Context) error {
		return srv.Shutdown(ctx)
	}
)

// SignalContext returns a context cancelled on SIGINT or SIGTERM.
// [Lifecycle]:
// This is the standard way to hook OS signals into the Go context tree.
// Passing this context to server.Run allows the server to stop accepting new connections
// immediately upon receiving Ctrl+C (SIGINT) or Kubernetes termination (SIGTERM).
func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

// Run starts the server and performs a graceful shutdown on ctx.Done().
// [Pattern: Graceful Shutdown]
//  1. Start server in a goroutine suitable for blocked ListenAndServe().
//  2. Block on select{} waiting for either:
//     a) Context cancellation (OS signal) -> Trigger Shutdown().
//     b) Server error (e.g., port in use) -> Return error immediately.
func Run(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration) error {
	if srv == nil {
		return errors.New("nil server")
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}

	errCh := make(chan error, 1)
	go func() {
		if err := listenAndServe(srv); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return shutdownServer(srv, shutdownCtx)
	case err := <-errCh:
		return err
	}
}
