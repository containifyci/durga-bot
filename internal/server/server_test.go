package server

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return strconv.Itoa(port)
}

func TestNewMux_RegistersWebhookRoute(t *testing.T) {
	t.Parallel()

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mux := NewMux(handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", http.NoBody)

	mux.ServeHTTP(rec, req)

	assert.True(t, called, "webhook handler should have been invoked")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewMux_RejectsNonPost(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux := NewMux(handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/webhooks/github", http.NoBody)

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestNew_SetsFields(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	logger := noopLogger()
	srv := New(handler, "9090", logger)

	assert.NotNil(t, srv)
	assert.Equal(t, ":9090", srv.srv.Addr)
	assert.Equal(t, 10*time.Second, srv.srv.ReadHeaderTimeout)
}

func TestRun_PortInUse(t *testing.T) {
	t.Parallel()

	// Occupy a port.
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)

	mux := http.NewServeMux()
	srv := New(mux, port, noopLogger())

	err = srv.Run()
	assert.Error(t, err)
}

//nolint:paralleltest // sends SIGINT to the process
func TestRun_ShutdownError(t *testing.T) {
	port := freePort(t)

	// Handler that blocks long enough for the tiny shutdown timeout to expire.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	mux.Handle("POST /webhooks/github", handler)

	srv := New(mux, port, noopLogger())
	srv.shutdownTimeout = 1 * time.Millisecond

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run() }()

	// Wait for server to accept TCP connections (don't use HTTP — the slow handler would block).
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", "localhost:"+port, 100*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}, 2*time.Second, 50*time.Millisecond)

	// Start a slow request that will be in-flight during shutdown.
	go func() {
		http.Post("http://localhost:"+port+"/webhooks/github", "", http.NoBody) //nolint:errcheck,noctx // fire-and-forget
	}()
	time.Sleep(100 * time.Millisecond) // let the request reach the handler

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGINT))

	select {
	case err := <-errCh:
		assert.ErrorContains(t, err, "shutting down server")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within timeout after SIGINT")
	}
}

//nolint:paralleltest // sends SIGINT to the process
func TestRun_GracefulShutdown(t *testing.T) {
	port := freePort(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	mux.Handle("POST /webhooks/github", handler)

	srv := New(mux, port, noopLogger())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run() }()

	// Wait for server to be ready.
	addr := "http://localhost:" + port + "/webhooks/github"
	require.Eventually(t, func() bool {
		resp, err := http.Post(addr, "", http.NoBody) //nolint:noctx // test-only convenience
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, 2*time.Second, 50*time.Millisecond)

	// Send shutdown signal.
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGINT))

	// Wait for Run to return.
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within timeout after SIGINT")
	}
}
