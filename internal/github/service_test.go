package github

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/containifyci/durga-bot/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveServiceName_FileWithServiceName(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, testutil.ContentsResponse("serviceName: my-custom-service\n"))
	})
	client := testutil.NewGitHubClient(t, mux)

	name, err := ResolveServiceName(context.Background(), client, "owner", "myrepo")

	require.NoError(t, err)
	assert.Equal(t, "my-custom-service", name)
}

func TestResolveServiceName_FileWithEmptyServiceName(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, testutil.ContentsResponse("serviceName: \"\"\n"))
	})
	client := testutil.NewGitHubClient(t, mux)

	name, err := ResolveServiceName(context.Background(), client, "owner", "myrepo")

	require.NoError(t, err)
	assert.Equal(t, "myrepo", name)
}

func TestResolveServiceName_FileNotFound(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	client := testutil.NewGitHubClient(t, mux)

	name, err := ResolveServiceName(context.Background(), client, "owner", "myrepo")

	require.NoError(t, err)
	assert.Equal(t, "myrepo", name)
}

func TestResolveServiceName_InvalidYAML(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, testutil.ContentsResponse("{{invalid yaml"))
	})
	client := testutil.NewGitHubClient(t, mux)

	_, err := ResolveServiceName(context.Background(), client, "owner", "myrepo")

	assert.ErrorContains(t, err, "parsing .github/.secret-token.yaml")
}

func TestResolveServiceName_InvalidEncoding(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"type":"file","encoding":"unsupported","content":"data"}`)
	})
	client := testutil.NewGitHubClient(t, mux)

	_, err := ResolveServiceName(context.Background(), client, "owner", "myrepo")

	assert.ErrorContains(t, err, "decoding file content")
}

func TestResolveServiceName_APIError(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/myrepo/contents/.github/.secret-token.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"Internal Server Error"}`)
	})
	client := testutil.NewGitHubClient(t, mux)

	_, err := ResolveServiceName(context.Background(), client, "owner", "myrepo")

	assert.ErrorContains(t, err, "fetching .github/.secret-token.yaml")
}
