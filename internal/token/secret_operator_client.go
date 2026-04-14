package token

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	gh "github.com/google/go-github/v67/github"
)

const tokenTTL = 15 * time.Minute
const maxRepoLocks = 1000

type repoLock struct {
	mu       sync.Mutex
	lastUsed time.Time
}

// PRTokenEntry is one entry in the PR-to-token map stored as a GitHub variable.
type PRTokenEntry struct {
	Token     string `json:"token"`
	Service   string `json:"service"`
	CreatedAt string `json:"created_at"`
}

// PRTokenMap is keyed by PR number (as string). Key "0" is used for non-PR events.
type PRTokenMap map[string]PRTokenEntry

type generateTokenRequest struct {
	ServiceName string `json:"serviceName"`
}

type generateTokenResponse struct {
	Token string `json:"token"`
}

type SecretOperatorClient struct {
	ghClient           *gh.Client
	httpClient         *http.Client
	logger             *slog.Logger
	secretOperatorHost string
	variableName       string
	repoLocksMu       sync.Mutex
	repoLocks         map[string]*repoLock
	maxLocks          int
}

func NewSecretOperatorClient(ghClient *gh.Client, secretOperatorHost, variableName string, logger *slog.Logger) *SecretOperatorClient {
	return &SecretOperatorClient{
		ghClient:           ghClient,
		httpClient:         &http.Client{Timeout: 10 * time.Second},
		secretOperatorHost: secretOperatorHost,
		variableName:       variableName,
		logger:             logger,
		repoLocks:         make(map[string]*repoLock),
		maxLocks:          maxRepoLocks,
	}
}

func (c *SecretOperatorClient) CreateToken(ctx context.Context, req TokenRequest) error {
	tokenStr, err := c.requestToken(ctx, req.ServiceName)
	if err != nil {
		return fmt.Errorf("requesting token from secret-operator: %w", err)
	}

	if err := c.saveAsGitHubVariable(ctx, req, tokenStr); err != nil {
		return fmt.Errorf("saving GitHub variable: %w", err)
	}

	c.logger.Info("token saved as GitHub variable",
		slog.String("repo", req.RepoOwner+"/"+req.RepoName),
		slog.String("variable", c.variableName),
		slog.String("service_name", req.ServiceName),
		slog.Int("pr_number", req.PRNumber),
	)

	return nil
}

func (c *SecretOperatorClient) requestToken(ctx context.Context, serviceName string) (string, error) {
	body, err := json.Marshal(generateTokenRequest{ServiceName: serviceName})
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	url := c.secretOperatorHost + "/generate-token"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("calling secret-operator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("secret-operator returned %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp generateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	if tokenResp.Token == "" {
		return "", fmt.Errorf("secret-operator returned empty token")
	}

	return tokenResp.Token, nil
}

func (c *SecretOperatorClient) repoMutex(owner, repo string) *sync.Mutex {
	key := owner + "/" + repo
	c.repoLocksMu.Lock()
	defer c.repoLocksMu.Unlock()

	rl, ok := c.repoLocks[key]
	if !ok {
		if len(c.repoLocks) >= c.maxLocks {
			c.evictOldestLock()
		}
		rl = &repoLock{}
		c.repoLocks[key] = rl
	}
	rl.lastUsed = time.Now()
	return &rl.mu
}

func (c *SecretOperatorClient) evictOldestLock() {
	var oldestKey string
	var oldestTime time.Time
	for k, rl := range c.repoLocks {
		if oldestKey == "" || rl.lastUsed.Before(oldestTime) {
			oldestKey = k
			oldestTime = rl.lastUsed
		}
	}
	delete(c.repoLocks, oldestKey)
}

func (c *SecretOperatorClient) saveAsGitHubVariable(ctx context.Context, req TokenRequest, tokenStr string) error {
	mu := c.repoMutex(req.RepoOwner, req.RepoName)
	mu.Lock()
	defer mu.Unlock()

	tokens, exists, err := c.readVariable(ctx, req.RepoOwner, req.RepoName)
	if err != nil {
		return err
	}

	// Prune expired entries
	now := time.Now()
	for k, entry := range tokens {
		created, parseErr := time.Parse(time.RFC3339, entry.CreatedAt)
		if parseErr != nil || now.Sub(created) > tokenTTL {
			delete(tokens, k)
		}
	}

	prKey := strconv.Itoa(req.PRNumber)
	tokens[prKey] = PRTokenEntry{
		Token:     tokenStr,
		Service:   req.ServiceName,
		CreatedAt: now.Format(time.RFC3339),
	}

	value, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("marshaling token map: %w", err)
	}

	if exists {
		_, err = c.ghClient.Actions.UpdateRepoVariable(ctx, req.RepoOwner, req.RepoName, &gh.ActionsVariable{
			Name:  c.variableName,
			Value: string(value),
		})
		if err != nil {
			return fmt.Errorf("updating variable: %w", err)
		}
	} else {
		_, err = c.ghClient.Actions.CreateRepoVariable(ctx, req.RepoOwner, req.RepoName, &gh.ActionsVariable{
			Name:  c.variableName,
			Value: string(value),
		})
		if err != nil {
			return fmt.Errorf("creating variable: %w", err)
		}
	}

	return nil
}

func (c *SecretOperatorClient) readVariable(ctx context.Context, owner, repo string) (PRTokenMap, bool, error) {
	variable, resp, err := c.ghClient.Actions.GetRepoVariable(ctx, owner, repo, c.variableName)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return make(PRTokenMap), false, nil
		}
		return nil, false, fmt.Errorf("reading variable: %w", err)
	}

	var tokens PRTokenMap
	if err := json.Unmarshal([]byte(variable.Value), &tokens); err != nil {
		c.logger.Warn("corrupt variable value, starting fresh",
			slog.String("error", err.Error()),
		)
		return make(PRTokenMap), true, nil
	}

	return tokens, true, nil
}
