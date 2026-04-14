package testutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	gh "github.com/google/go-github/v67/github"
	"github.com/stretchr/testify/require"
)

// DiscardLogger returns a logger that writes to io.Discard.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// NewGitHubClient creates a *gh.Client backed by an httptest.Server serving the given handler.
// The server is automatically closed when the test finishes.
func NewGitHubClient(t *testing.T, handler http.Handler) *gh.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := gh.NewClient(nil)
	baseURL, err := url.Parse(srv.URL + "/")
	require.NoError(t, err)
	client.BaseURL = baseURL
	return client
}

// FreePort returns an available TCP port as a string.
func FreePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return strconv.Itoa(port)
}

// GenerateRSAKey generates a 2048-bit RSA private key in PEM format.
func GenerateRSAKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

// ContentsResponse returns a GitHub API content response JSON with the given content base64-encoded.
func ContentsResponse(content string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	return fmt.Sprintf(`{"type":"file","encoding":"base64","content":"%s"}`, encoded)
}
