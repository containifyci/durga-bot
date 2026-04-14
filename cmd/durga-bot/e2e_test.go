package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	githubinternal "github.com/containifyci/durga-bot/internal/github"
	"github.com/containifyci/durga-bot/internal/testutil"
	"github.com/containifyci/durga-bot/internal/token"
	gh "github.com/google/go-github/v67/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockSecretOperator(t *testing.T, tokenValue string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"%s"}`, tokenValue)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestE2E_PRTokenFlowWithCustomServiceName tests the full flow for a PR event:
// resolve service name → request token from secret-operator → save in GitHub variable.
func TestE2E_PRTokenFlowWithCustomServiceName(t *testing.T) {
	t.Parallel()

	soHost := mockSecretOperator(t, "e2e-token-42")
	var createdVariable string

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testorg/testrepo/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, testutil.ContentsResponse("serviceName: my-custom-svc\n"))
	})
	mux.HandleFunc("/repos/testorg/testrepo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	mux.HandleFunc("/repos/testorg/testrepo/actions/variables", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var v gh.ActionsVariable
			_ = json.Unmarshal(body, &v)
			createdVariable = v.Value
			w.WriteHeader(http.StatusCreated)
		}
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	ctx := context.Background()

	serviceName, err := githubinternal.ResolveServiceName(ctx, ghClient, "testorg", "testrepo")
	require.NoError(t, err)
	assert.Equal(t, "my-custom-svc", serviceName)

	tokenCli := token.NewSecretOperatorClient(ghClient, soHost, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())
	err = tokenCli.CreateToken(ctx, token.TokenRequest{
		ServiceName: serviceName,
		RepoOwner:   "testorg",
		RepoName:    "testrepo",
		PRNumber:    42,
	})
	require.NoError(t, err)

	var tokens token.PRTokenMap
	require.NoError(t, json.Unmarshal([]byte(createdVariable), &tokens))
	assert.Contains(t, tokens, "42")
	assert.Equal(t, "my-custom-svc", tokens["42"].Service)
	assert.Equal(t, "e2e-token-42", tokens["42"].Token)
}

// TestE2E_PRTokenFlowFallbackToRepoName verifies fallback to repo name.
func TestE2E_PRTokenFlowFallbackToRepoName(t *testing.T) {
	t.Parallel()

	soHost := mockSecretOperator(t, "e2e-fallback-token")
	var createdVariable string

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/myorg/myapp/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	mux.HandleFunc("/repos/myorg/myapp/actions/variables/MY_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	mux.HandleFunc("/repos/myorg/myapp/actions/variables", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var v gh.ActionsVariable
			_ = json.Unmarshal(body, &v)
			createdVariable = v.Value
			w.WriteHeader(http.StatusCreated)
		}
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	ctx := context.Background()

	serviceName, err := githubinternal.ResolveServiceName(ctx, ghClient, "myorg", "myapp")
	require.NoError(t, err)
	assert.Equal(t, "myapp", serviceName)

	tokenCli := token.NewSecretOperatorClient(ghClient, soHost, "MY_TOKENS", testutil.DiscardLogger())
	err = tokenCli.CreateToken(ctx, token.TokenRequest{
		ServiceName: serviceName,
		RepoOwner:   "myorg",
		RepoName:    "myapp",
		PRNumber:    7,
	})
	require.NoError(t, err)

	var tokens token.PRTokenMap
	require.NoError(t, json.Unmarshal([]byte(createdVariable), &tokens))
	assert.Contains(t, tokens, "7")
	assert.Equal(t, "myapp", tokens["7"].Service)
}

// TestE2E_FullIntegration requires a real secret-operator running at SECRET_OPERATOR_URL.
// Run with: SECRET_OPERATOR_URL=http://localhost:9999 go test ./cmd/durga-bot/ -run TestE2E_FullIntegration -v
func TestE2E_FullIntegration(t *testing.T) {
	soURL := os.Getenv("SECRET_OPERATOR_URL")
	if soURL == "" {
		t.Skip("SECRET_OPERATOR_URL not set, skipping full integration test")
	}

	var createdVariable string

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testorg/integration-repo/actions/variables/SECRET_OPERATOR_TOKENS", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	mux.HandleFunc("/repos/testorg/integration-repo/actions/variables", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var v gh.ActionsVariable
			_ = json.Unmarshal(body, &v)
			createdVariable = v.Value
			w.WriteHeader(http.StatusCreated)
		}
	})

	ghClient := testutil.NewGitHubClient(t, mux)
	ctx := context.Background()

	tokenCli := token.NewSecretOperatorClient(ghClient, soURL, "SECRET_OPERATOR_TOKENS", testutil.DiscardLogger())

	err := tokenCli.CreateToken(ctx, token.TokenRequest{
		ServiceName: "integration-test-service2",
		RepoOwner:   "testorg",
		RepoName:    "integration-repo",
		PRNumber:    99,
	})
	require.NoError(t, err)

	var tokens token.PRTokenMap
	require.NoError(t, json.Unmarshal([]byte(createdVariable), &tokens))
	assert.Contains(t, tokens, "99")
	assert.Equal(t, "integration-test-service2", tokens["99"].Service)
	assert.NotEmpty(t, tokens["99"].Token, "token from secret-operator should not be empty")

	t.Logf("Token received from secret-operator: %s", tokens["99"].Token)
}

// TestE2E_GitHubVariableIntegration creates a real GitHub Actions variable
// using a personal access token. Requires GITHUB_TOKEN and GITHUB_TEST_REPO.
//
// Run with:
//
//	GITHUB_TOKEN=ghp_... \
//	GITHUB_TEST_REPO=containifyci/durga-bot \
//	go test ./cmd/durga-bot/ -run TestE2E_GitHubVariableIntegration -v
func TestE2E_GitHubVariableIntegration(t *testing.T) {
	ghToken := os.Getenv("GITHUB_TOKEN")
	testRepo := os.Getenv("GITHUB_TEST_REPO")

	if ghToken == "" || testRepo == "" {
		t.Skip("GITHUB_TOKEN or GITHUB_TEST_REPO not set, skipping GitHub variable integration test")
	}

	owner, repo, ok := strings.Cut(testRepo, "/")
	require.True(t, ok, "GITHUB_TEST_REPO must be in owner/repo format")

	ghClient := gh.NewClient(nil).WithAuthToken(ghToken)
	ctx := context.Background()
	variableName := "DURGA_BOT_E2E_TEST"

	// Clean up before and after
	_, _ = ghClient.Actions.DeleteRepoVariable(ctx, owner, repo, variableName)

	soHost := mockSecretOperator(t, "github-integration-token")

	tokenCli := token.NewSecretOperatorClient(ghClient, soHost, variableName, testutil.DiscardLogger())

	// --- PR #100: creates the variable ---
	err := tokenCli.CreateToken(ctx, token.TokenRequest{
		ServiceName: "e2e-github-test",
		RepoOwner:   owner,
		RepoName:    repo,
		PRNumber:    100,
	})
	require.NoError(t, err, "CreateToken for PR #100 failed")

	variable, _, err := ghClient.Actions.GetRepoVariable(ctx, owner, repo, variableName)
	require.NoError(t, err, "variable should exist after first CreateToken")

	var tokens token.PRTokenMap
	require.NoError(t, json.Unmarshal([]byte(variable.Value), &tokens))
	assert.Contains(t, tokens, "100")
	assert.Equal(t, "e2e-github-test", tokens["100"].Service)
	assert.Equal(t, "github-integration-token", tokens["100"].Token)
	t.Logf("After PR #100: %s", variable.Value)

	// --- PR #200: updates the variable, both PRs present ---
	err = tokenCli.CreateToken(ctx, token.TokenRequest{
		ServiceName: "e2e-github-test",
		RepoOwner:   owner,
		RepoName:    repo,
		PRNumber:    200,
	})
	require.NoError(t, err, "CreateToken for PR #200 failed")

	variable, _, err = ghClient.Actions.GetRepoVariable(ctx, owner, repo, variableName)
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal([]byte(variable.Value), &tokens))
	assert.Contains(t, tokens, "100", "PR #100 should still be present")
	assert.Contains(t, tokens, "200", "PR #200 should be added")
	assert.Equal(t, "github-integration-token", tokens["200"].Token)
	t.Logf("After PR #200: %s", variable.Value)
}
