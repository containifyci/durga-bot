package token

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/containifyci/durga-bot/internal/testutil"
	gh "github.com/google/go-github/v67/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMockSecretOperator(t *testing.T, tokenValue string, statusCode int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/generate-token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req generateTokenRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			fmt.Fprint(w, `{"error":"failed"}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(generateTokenResponse{Token: tokenValue})
		require.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestSecretOperatorClient_CreateToken_NewVariable(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "test-token-abc", http.StatusOK)
	variableCreated := false

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	mux.HandleFunc("/repos/owner/myrepo/actions/variables", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			variableCreated = true
			w.WriteHeader(http.StatusCreated)
		}
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	client := NewSecretOperatorClient(ghClient, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := client.CreateToken(context.Background(), TokenRequest{
		ServiceName: "my-service",
		RepoOwner:   "owner",
		RepoName:    "myrepo",
		PRNumber:    42,
	})

	require.NoError(t, err)
	assert.True(t, variableCreated, "GitHub variable should have been created")
}

func TestSecretOperatorClient_CreateToken_UpdateExistingVariable(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "new-token-xyz", http.StatusOK)

	existingTokens := PRTokenMap{
		"10": {
			Token:     "existing-token",
			Service:   "other-service",
			CreatedAt: time.Now().Format(time.RFC3339),
		},
	}
	existingJSON, _ := json.Marshal(existingTokens)

	var updatedValue string
	variableUpdated := false

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"name":"SECRET_OPERATOR_TOKENS","value":%q}`, string(existingJSON))
		case http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			var v gh.ActionsVariable
			_ = json.Unmarshal(body, &v)
			updatedValue = v.Value
			variableUpdated = true
			w.WriteHeader(http.StatusNoContent)
		}
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	client := NewSecretOperatorClient(ghClient, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := client.CreateToken(context.Background(), TokenRequest{
		ServiceName: "my-service",
		RepoOwner:   "owner",
		RepoName:    "myrepo",
		PRNumber:    42,
	})

	require.NoError(t, err)
	assert.True(t, variableUpdated)

	var result PRTokenMap
	require.NoError(t, json.Unmarshal([]byte(updatedValue), &result))
	assert.Contains(t, result, "10", "existing PR entry should be preserved")
	assert.Contains(t, result, "42", "new PR entry should be added")
	assert.Equal(t, "new-token-xyz", result["42"].Token)
	assert.Equal(t, "my-service", result["42"].Service)
}

func TestSecretOperatorClient_CreateToken_PrunesExpiredEntries(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "fresh-token", http.StatusOK)

	existingTokens := PRTokenMap{
		"10": {Token: "old", Service: "old", CreatedAt: time.Now().Add(-20 * time.Minute).Format(time.RFC3339)},
		"20": {Token: "fresh", Service: "fresh", CreatedAt: time.Now().Format(time.RFC3339)},
	}
	existingJSON, _ := json.Marshal(existingTokens)

	var updatedValue string

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"name":"SECRET_OPERATOR_TOKENS","value":%q}`, string(existingJSON))
		case http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			var v gh.ActionsVariable
			_ = json.Unmarshal(body, &v)
			updatedValue = v.Value
			w.WriteHeader(http.StatusNoContent)
		}
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	client := NewSecretOperatorClient(ghClient, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := client.CreateToken(context.Background(), TokenRequest{
		ServiceName: "new", RepoOwner: "owner", RepoName: "myrepo", PRNumber: 30,
	})
	require.NoError(t, err)

	var result PRTokenMap
	require.NoError(t, json.Unmarshal([]byte(updatedValue), &result))
	assert.NotContains(t, result, "10", "expired entry should be pruned")
	assert.Contains(t, result, "20", "fresh entry should be preserved")
	assert.Contains(t, result, "30", "new entry should be added")
}

func TestSecretOperatorClient_CreateToken_SecretOperatorError(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "", http.StatusInternalServerError)

	client := NewSecretOperatorClient(nil, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := client.CreateToken(context.Background(), TokenRequest{
		ServiceName: "my-service", RepoOwner: "owner", RepoName: "myrepo", PRNumber: 1,
	})

	assert.ErrorContains(t, err, "secret-operator returned 500")
}

func TestSecretOperatorClient_CreateToken_ReadVariableError(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "token-abc", http.StatusOK)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	client := NewSecretOperatorClient(ghClient, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := client.CreateToken(context.Background(), TokenRequest{
		ServiceName: "my-service", RepoOwner: "owner", RepoName: "myrepo", PRNumber: 1,
	})

	assert.ErrorContains(t, err, "reading variable")
}

func TestRequestToken_Success(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "my-generated-token", http.StatusOK)
	client := NewSecretOperatorClient(nil, soHost, "TEST", testutil.DiscardLogger())

	token, err := client.requestToken(context.Background(), "my-service")

	require.NoError(t, err)
	assert.Equal(t, "my-generated-token", token)
}

func TestRequestToken_EmptyToken(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "", http.StatusOK)
	client := NewSecretOperatorClient(nil, soHost, "TEST", testutil.DiscardLogger())

	_, err := client.requestToken(context.Background(), "my-service")

	assert.ErrorContains(t, err, "empty token")
}

func TestRequestToken_ConnectionError(t *testing.T) {
	t.Parallel()

	client := NewSecretOperatorClient(nil, "http://127.0.0.1:1", "TEST", testutil.DiscardLogger())
	_, err := client.requestToken(context.Background(), "my-service")

	assert.ErrorContains(t, err, "calling secret-operator")
}

func TestRequestToken_InvalidResponseBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `not valid json`)
	}))
	t.Cleanup(srv.Close)

	client := NewSecretOperatorClient(nil, srv.URL, "TEST", testutil.DiscardLogger())
	_, err := client.requestToken(context.Background(), "my-service")

	assert.ErrorContains(t, err, "decoding response")
}

func TestSecretOperatorClient_CreateToken_CreateVariableError(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "test-token", http.StatusOK)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	mux.HandleFunc("/repos/owner/myrepo/actions/variables", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"Internal Server Error"}`)
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	client := NewSecretOperatorClient(ghClient, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := client.CreateToken(context.Background(), TokenRequest{
		ServiceName: "my-service", RepoOwner: "owner", RepoName: "myrepo", PRNumber: 1,
	})

	assert.ErrorContains(t, err, "creating variable")
}

func TestSecretOperatorClient_CreateToken_UpdateVariableError(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "test-token", http.StatusOK)

	existingTokens := PRTokenMap{
		"10": {Token: "existing", Service: "svc", CreatedAt: time.Now().Format(time.RFC3339)},
	}
	existingJSON, _ := json.Marshal(existingTokens)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"name":"SECRET_OPERATOR_TOKENS","value":%q}`, string(existingJSON))
		case http.MethodPatch:
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"Internal Server Error"}`)
		}
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	client := NewSecretOperatorClient(ghClient, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := client.CreateToken(context.Background(), TokenRequest{
		ServiceName: "my-service", RepoOwner: "owner", RepoName: "myrepo", PRNumber: 42,
	})

	assert.ErrorContains(t, err, "updating variable")
}

func TestSecretOperatorClient_CreateToken_CorruptVariableValue(t *testing.T) {
	t.Parallel()

	soHost := setupMockSecretOperator(t, "fresh-token", http.StatusOK)
	var updatedValue string

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"name":"SECRET_OPERATOR_TOKENS","value":"not valid json"}`)
		case http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			var v gh.ActionsVariable
			_ = json.Unmarshal(body, &v)
			updatedValue = v.Value
			w.WriteHeader(http.StatusNoContent)
		}
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	client := NewSecretOperatorClient(ghClient, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := client.CreateToken(context.Background(), TokenRequest{
		ServiceName: "my-service", RepoOwner: "owner", RepoName: "myrepo", PRNumber: 5,
	})

	require.NoError(t, err)

	var result PRTokenMap
	require.NoError(t, json.Unmarshal([]byte(updatedValue), &result))
	assert.Contains(t, result, "5")
	assert.Equal(t, "fresh-token", result["5"].Token)
}

func TestRepoMutex_EvictsOldEntries(t *testing.T) {
	t.Parallel()

	client := NewSecretOperatorClient(nil, "", "TEST", testutil.DiscardLogger())
	client.maxLocks = 3

	// Fill to capacity.
	client.repoMutex("org", "repo1")
	time.Sleep(time.Millisecond)
	client.repoMutex("org", "repo2")
	time.Sleep(time.Millisecond)
	client.repoMutex("org", "repo3")

	assert.Len(t, client.repoLocks, 3)

	// Adding a 4th should evict repo1 (oldest lastUsed).
	client.repoMutex("org", "repo4")

	assert.Len(t, client.repoLocks, 3)
	assert.NotContains(t, client.repoLocks, "org/repo1")
	assert.Contains(t, client.repoLocks, "org/repo2")
	assert.Contains(t, client.repoLocks, "org/repo3")
	assert.Contains(t, client.repoLocks, "org/repo4")
}

func TestRepoMutex_RefreshKeepsEntry(t *testing.T) {
	t.Parallel()

	client := NewSecretOperatorClient(nil, "", "TEST", testutil.DiscardLogger())
	client.maxLocks = 3

	client.repoMutex("org", "repo1")
	time.Sleep(time.Millisecond)
	client.repoMutex("org", "repo2")
	time.Sleep(time.Millisecond)
	client.repoMutex("org", "repo3")

	// Refresh repo1 so it's no longer the oldest.
	time.Sleep(time.Millisecond)
	client.repoMutex("org", "repo1")

	// Adding repo4 should evict repo2 (now the oldest).
	client.repoMutex("org", "repo4")

	assert.Len(t, client.repoLocks, 3)
	assert.Contains(t, client.repoLocks, "org/repo1")
	assert.NotContains(t, client.repoLocks, "org/repo2")
	assert.Contains(t, client.repoLocks, "org/repo3")
	assert.Contains(t, client.repoLocks, "org/repo4")
}
