package token

import (
	"context"
)

// TokenRequest contains the parameters needed to create and store a token.
type TokenRequest struct {
	ServiceName string // from .github/.secret-token.yaml or repo name
	RepoOwner   string
	RepoName    string
	PRNumber    int // 0 for non-PR events (push, etc.)
}

type Client interface {
	CreateToken(ctx context.Context, req TokenRequest) error
}
