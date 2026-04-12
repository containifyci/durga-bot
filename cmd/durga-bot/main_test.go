package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/containifyci/durga-bot/internal/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestRSAKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return strconv.Itoa(port)
}

func setValidEnv(t *testing.T, pemKey string) {
	t.Helper()
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_INSTALLATION_ID", "2")
	t.Setenv("GITHUB_PRIVATE_KEY", pemKey)
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret")
}

func newTestApp() app {
	return app{
		newTokenCli: func() token.Client { return nil },
	}
}

// --- run() tests ---

func TestRun_ConfigError(t *testing.T) {
	// No env vars set → config.Load() fails.
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_INSTALLATION_ID", "")
	t.Setenv("GITHUB_PRIVATE_KEY", "")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "")

	a := newTestApp()
	err := a.run()

	assert.ErrorContains(t, err, "loading config")
}

func TestRun_GitHubClientError(t *testing.T) {
	// Valid config but invalid PEM key (>= 10 chars to exercise debug-log branch).
	setValidEnv(t, "invalid-pem-key-long-enough")

	a := newTestApp()
	err := a.run()

	assert.ErrorContains(t, err, "creating GitHub client")
}

func TestRun_ServerError(t *testing.T) {
	// Valid config with real RSA key but PORT is already occupied.
	pemKey := generateTestRSAKey(t)
	setValidEnv(t, pemKey)

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	t.Setenv("PORT", port)

	a := newTestApp()
	err = a.run()

	assert.ErrorContains(t, err, "running server")
}

//nolint:paralleltest // sends SIGINT to the process and uses t.Setenv
func TestRun_GracefulShutdown(t *testing.T) {
	pemKey := generateTestRSAKey(t)
	port := freePort(t)
	setValidEnv(t, pemKey)
	t.Setenv("PORT", port)

	a := newTestApp()
	errCh := make(chan error, 1)
	go func() { errCh <- a.run() }()

	// Wait for server to be ready.
	addr := "http://localhost:" + port + "/webhooks/github"
	require.Eventually(t, func() bool {
		resp, err := http.Post(addr, "", http.NoBody) //nolint:noctx // test-only convenience
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, 3*time.Second, 50*time.Millisecond)

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGINT))

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return within timeout after SIGINT")
	}
}

// --- appMain() tests ---

func TestAppMain_RunError(t *testing.T) {
	// Config will fail → run() errors → appMain returns 1.
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_INSTALLATION_ID", "")
	t.Setenv("GITHUB_PRIVATE_KEY", "")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "")
	a := newTestApp()
	code := a.appMain()

	assert.Equal(t, 1, code)
}

//nolint:paralleltest // sends SIGINT to the process and uses t.Setenv
func TestAppMain_Success(t *testing.T) {
	pemKey := generateTestRSAKey(t)
	port := freePort(t)
	setValidEnv(t, pemKey)
	t.Setenv("PORT", port)
	a := newTestApp()
	codeCh := make(chan int, 1)
	go func() { codeCh <- a.appMain() }()

	// Wait for webhook server to be ready.
	addr := "http://localhost:" + port + "/webhooks/github"
	require.Eventually(t, func() bool {
		resp, err := http.Post(addr, "", http.NoBody) //nolint:noctx // test-only convenience
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, 3*time.Second, 50*time.Millisecond)

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGINT))

	select {
	case code := <-codeCh:
		assert.Equal(t, 0, code)
	case <-time.After(5 * time.Second):
		t.Fatal("appMain() did not return within timeout after SIGINT")
	}
}
