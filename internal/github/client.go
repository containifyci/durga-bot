package github

import (
	"fmt"
	"net/http"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v67/github"
)

// NewInstallationClient creates a GitHub API client authenticated as a GitHub App installation.
func NewInstallationClient(appID, installationID int64, privateKey []byte) (*gh.Client, error) {
	tr, err := ghinstallation.New(
		http.DefaultTransport,
		appID,
		installationID,
		privateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("creating github app installation transport: %w", err)
	}
	return gh.NewClient(&http.Client{Transport: tr}), nil
}
