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

// SignalContext returns a context cancelled on SIGINT or SIGTERM.
func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

// Run starts the server and performs a graceful shutdown on ctx.Done().
func Run(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration) error {
	if srv == nil {
		return errors.New("nil server")
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
