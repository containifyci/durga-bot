package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret"

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func signPayload(t *testing.T, secret, payload []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	_, err := mac.Write(payload)
	require.NoError(t, err)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func buildHandler() *Handler {
	return NewHandler(testSecret, noopLogger(), nil)
}

type mockTokenClient struct {
	mock.Mock
}

func (m *mockTokenClient) CreateToken(ctx context.Context, service string) error {
	return m.Called(ctx, service).Error(0)
}

func TestWebhook_InvalidSignature(t *testing.T) {
	t.Parallel()

	handler := buildHandler()
	payload := []byte(`{"action":"opened"}`)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalidsignature")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "test-delivery-1")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWebhook_ValidSignature_Returns200(t *testing.T) {
	t.Parallel()

	handler := buildHandler()
	payload := []byte(`{"action":"opened"}`)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signPayload(t, []byte(testSecret), payload))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "test-delivery-2")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "event received", rec.Body.String())
}

func TestWebhook_UnknownEvent_Returns200(t *testing.T) {
	t.Parallel()

	handler := buildHandler()
	payload := []byte(`{}`)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signPayload(t, []byte(testSecret), payload))
	req.Header.Set("X-GitHub-Event", "some_unknown_event")
	req.Header.Set("X-GitHub-Delivery", "test-delivery-3")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWebhook_WithTokenClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mockReturn func(*mockTokenClient, chan struct{})
		name       string
	}{
		{
			name: "creates token",
			mockReturn: func(mc *mockTokenClient, done chan struct{}) {
				mc.On("CreateToken",
					mock.Anything, "push:goflink/son-of-anton",
				).Run(func(_ mock.Arguments) { close(done) }).Return(nil)
			},
		},
		{
			name: "token error",
			mockReturn: func(mc *mockTokenClient, done chan struct{}) {
				mc.On("CreateToken",
					mock.Anything, "push:goflink/son-of-anton",
				).Run(func(_ mock.Arguments) { close(done) }).Return(errors.New("token creation failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mc := &mockTokenClient{}
			done := make(chan struct{})
			tt.mockReturn(mc, done)

			handler := NewHandler(testSecret, noopLogger(), mc)
			payload := []byte(`{"repository":{"full_name":"goflink/son-of-anton"}}`)

			req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Hub-Signature-256", signPayload(t, []byte(testSecret), payload))
			req.Header.Set("X-GitHub-Event", "push")
			req.Header.Set("X-GitHub-Delivery", "delivery-42")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("workflow goroutine did not complete within timeout")
			}
		})
	}
}

func TestExtractRepoName_ValidPayload(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"repository":{"full_name":"goflink/son-of-anton"}}`)
	assert.Equal(t, "goflink/son-of-anton", extractRepoName(payload))
}

func TestExtractRepoName_MissingRepo(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"action":"opened"}`)
	assert.Equal(t, "unknown", extractRepoName(payload))
}

func TestExtractRepoName_InvalidJSON(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "unknown", extractRepoName([]byte(`not json`)))
}
