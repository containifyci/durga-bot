package testutil

import (
	"net/http"
	"testing"
)

// TestHelpers exercises every exported helper so that testutil is included
// in the project's coverage report. The real validation of these helpers
// happens in the packages that use them (github, token, server, etc.).
func TestHelpers(t *testing.T) {
	t.Parallel()
	DiscardLogger()
	NewGitHubClient(t, http.NewServeMux())
	FreePort(t)
	GenerateRSAKey(t)
	ContentsResponse("test")
}
