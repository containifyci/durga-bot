package github

import (
	"context"
	"fmt"
	"net/http"

	gh "github.com/google/go-github/v67/github"
	"gopkg.in/yaml.v3"
)

type secretTokenConfig struct {
	ServiceName string `yaml:"serviceName"`
}

// ResolveServiceName reads .github/.secret-token.yaml from the repository.
// If the file exists and contains a serviceName field, that value is returned.
// Otherwise, the repository name is returned as the default.
func ResolveServiceName(ctx context.Context, client *gh.Client, owner, repo string) (string, error) {
	fileContent, _, resp, err := client.Repositories.GetContents(
		ctx, owner, repo, ".github/.secret-token.yaml",
		&gh.RepositoryContentGetOptions{},
	)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return repo, nil
		}
		return "", fmt.Errorf("fetching .github/.secret-token.yaml: %w", err)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return "", fmt.Errorf("decoding file content: %w", err)
	}

	var cfg secretTokenConfig
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return "", fmt.Errorf("parsing .github/.secret-token.yaml: %w", err)
	}

	if cfg.ServiceName == "" {
		return repo, nil
	}

	return cfg.ServiceName, nil
}
