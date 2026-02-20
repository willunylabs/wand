package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func stubRunHooks(
	t *testing.T,
	listenFn func(*http.Server) error,
	shutdownFn func(*http.Server, context.Context) error,
) {
	t.Helper()
	prevListen := listenAndServe
	prevShutdown := shutdownServer
	if listenFn != nil {
		listenAndServe = listenFn
	}
	if shutdownFn != nil {
		shutdownServer = shutdownFn
	}
	t.Cleanup(func() {
		listenAndServe = prevListen
		shutdownServer = prevShutdown
	})
}

func TestSignalContext_ParentCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx, stop := SignalContext(parent)
	defer stop()

	cancel()

	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected context to be canceled")
	}
}

func TestRun_NilServer(t *testing.T) {
	if err := Run(context.Background(), nil, time.Second); err == nil {
		t.Fatal("expected error for nil server")
	}
}

func TestRun_ListenError(t *testing.T) {
	srv := &http.Server{Addr: "127.0.0.1:bad"}
	if err := Run(context.Background(), srv, time.Second); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("network listen not permitted: %v", err)
		}
		t.Fatalf("listen failed: %v", err)
	}
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv := &http.Server{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, srv, 50*time.Millisecond)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	runErr := <-done
	if runErr != nil && !errors.Is(runErr, http.ErrServerClosed) {
		t.Fatalf("unexpected error: %v", runErr)
	}
}

func TestRun_GracefulShutdown_DefaultTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("network listen not permitted: %v", err)
		}
		t.Fatalf("listen failed: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}

	done := make(chan error, 1)
	go func() {
		// Pass zero to execute the default-timeout branch.
		done <- Run(ctx, srv, 0)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	runErr := <-done
	if runErr != nil && !errors.Is(runErr, http.ErrServerClosed) {
		t.Fatalf("unexpected error: %v", runErr)
	}
}

func TestRun_GracefulShutdown_Timeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("network listen not permitted: %v", err)
		}
		t.Fatalf("listen failed: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { close(started) })
			<-release
			w.WriteHeader(http.StatusOK)
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, srv, 10*time.Millisecond)
	}()

	reqDone := make(chan error, 1)
	go func() {
		resp, reqErr := http.Get("http://" + addr)
		if reqErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		reqDone <- reqErr
	}()

	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		cancel()
		close(release)
		t.Fatal("handler did not start in time")
	}

	cancel()
	runErr := <-runDone
	if !errors.Is(runErr, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("expected context deadline exceeded, got %v", runErr)
	}

	close(release)
	_ = srv.Close()
	select {
	case <-reqDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("request did not finish in time")
	}
}

func TestRun_DefaultTimeoutAndShutdownBranch(t *testing.T) {
	started := make(chan struct{})
	releaseListen := make(chan struct{})
	shutdownCalled := make(chan time.Time, 1)
	shutdownErr := errors.New("shutdown-error")

	stubRunHooks(
		t,
		func(*http.Server) error {
			close(started)
			<-releaseListen
			return http.ErrServerClosed
		},
		func(_ *http.Server, ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("expected shutdown context deadline")
			}
			shutdownCalled <- deadline
			return shutdownErr
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, &http.Server{}, 0)
	}()

	<-started
	cancel()
	err := <-done
	if !errors.Is(err, shutdownErr) {
		t.Fatalf("expected shutdown error, got %v", err)
	}

	deadline := <-shutdownCalled
	remaining := time.Until(deadline)
	if remaining < 4500*time.Millisecond || remaining > 5500*time.Millisecond {
		t.Fatalf("expected default ~5s timeout, got remaining %s", remaining)
	}
	close(releaseListen)
}

func TestRun_ErrServerClosedMapsToNil(t *testing.T) {
	stubRunHooks(
		t,
		func(*http.Server) error {
			return http.ErrServerClosed
		},
		nil,
	)

	if err := Run(context.Background(), &http.Server{}, time.Second); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
