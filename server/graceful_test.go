package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

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
