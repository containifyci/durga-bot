package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/containifyci/durga-bot/internal/testutil"
	"github.com/containifyci/durga-bot/internal/token"
	gh "github.com/google/go-github/v67/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret"

func signPayload(t *testing.T, secret, payload []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	_, err := mac.Write(payload)
	require.NoError(t, err)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func buildHandler() *Handler {
	return NewHandler(testSecret, testutil.DiscardLogger(), nil, nil)
}

type mockTokenClient struct {
	mock.Mock
}

func (m *mockTokenClient) CreateToken(ctx context.Context, req token.TokenRequest) error {
	return m.Called(ctx, req).Error(0)
}

// notFoundGitHubClient returns a GitHub client that returns 404 for all requests,
// so ResolveServiceName falls back to the repo name.
func notFoundGitHubClient(t *testing.T) *gh.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	return testutil.NewGitHubClient(t, mux)
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
					mock.Anything, token.TokenRequest{
						ServiceName: "son-of-anton",
						RepoOwner:   "goflink",
						RepoName:    "son-of-anton",
					},
				).Run(func(_ mock.Arguments) { close(done) }).Return(nil)
			},
		},
		{
			name: "token error",
			mockReturn: func(mc *mockTokenClient, done chan struct{}) {
				mc.On("CreateToken",
					mock.Anything, token.TokenRequest{
						ServiceName: "son-of-anton",
						RepoOwner:   "goflink",
						RepoName:    "son-of-anton",
					},
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

			ghClient := notFoundGitHubClient(t)
			handler := NewHandler(testSecret, testutil.DiscardLogger(), mc, ghClient)
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

func TestWebhook_WithTokenClient_CustomServiceName(t *testing.T) {
	t.Parallel()

	mc := &mockTokenClient{}
	done := make(chan struct{})
	mc.On("CreateToken",
		mock.Anything, token.TokenRequest{
			ServiceName: "custom-service",
			RepoOwner:   "goflink",
			RepoName:    "son-of-anton",
		},
	).Run(func(_ mock.Arguments) { close(done) }).Return(nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/goflink/son-of-anton/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, testutil.ContentsResponse("serviceName: custom-service\n"))
	})
	ghClient := testutil.NewGitHubClient(t, mux)

	handler := NewHandler(testSecret, testutil.DiscardLogger(), mc, ghClient)
	payload := []byte(`{"repository":{"full_name":"goflink/son-of-anton"}}`)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signPayload(t, []byte(testSecret), payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-43")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("workflow goroutine did not complete within timeout")
	}
}

func TestWebhook_WithTokenClient_UnparseableRepoName(t *testing.T) {
	t.Parallel()

	mc := &mockTokenClient{}
	handler := NewHandler(testSecret, testutil.DiscardLogger(), mc, nil)
	payload := []byte(`{"repository":{"full_name":"noslash"}}`)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signPayload(t, []byte(testSecret), payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-44")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "event received", rec.Body.String())
	mc.AssertNotCalled(t, "CreateToken")
}

func TestWebhook_WithTokenClient_ResolveServiceNameError(t *testing.T) {
	t.Parallel()

	mc := &mockTokenClient{}
	done := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/goflink/son-of-anton/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"Internal Server Error"}`)
		close(done)
	})
	ghClient := testutil.NewGitHubClient(t, mux)

	handler := NewHandler(testSecret, testutil.DiscardLogger(), mc, ghClient)
	payload := []byte(`{"repository":{"full_name":"goflink/son-of-anton"}}`)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signPayload(t, []byte(testSecret), payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-45")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not hit the GitHub API within timeout")
	}
	mc.AssertNotCalled(t, "CreateToken")
}

func TestExtractWebhookPayload_ValidPayload(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"repository":{"full_name":"goflink/son-of-anton"},"number":42}`)
	wp := extractWebhookPayload(payload)
	assert.Equal(t, "goflink/son-of-anton", wp.Repository.FullName)
	assert.Equal(t, 42, wp.Number)
}

func TestExtractWebhookPayload_MissingRepo(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"action":"opened"}`)
	wp := extractWebhookPayload(payload)
	assert.Equal(t, "unknown", wp.Repository.FullName)
	assert.Equal(t, 0, wp.Number)
}

func TestExtractWebhookPayload_InvalidJSON(t *testing.T) {
	t.Parallel()
	wp := extractWebhookPayload([]byte(`not json`))
	assert.Equal(t, "", wp.Repository.FullName)
	assert.Equal(t, 0, wp.Number)
}

func TestExtractWebhookPayload_PushEvent(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"repository":{"full_name":"goflink/son-of-anton"}}`)
	wp := extractWebhookPayload(payload)
	assert.Equal(t, "goflink/son-of-anton", wp.Repository.FullName)
	assert.Equal(t, 0, wp.Number)
}
