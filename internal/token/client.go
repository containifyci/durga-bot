package token

import (
	"context"
)

// Client is an interface for creating tokens for services.
type Client interface {
	CreateToken(ctx context.Context, service string) error
}

// EchoClient is a Client implementation that echoes the service name.
type EchoClient struct{}

func NewClient() Client {
	return &EchoClient{}
}

// CreateToken implements the Client interface for EchoClient.
func (n *EchoClient) CreateToken(ctx context.Context, service string) error {
	return nil
}