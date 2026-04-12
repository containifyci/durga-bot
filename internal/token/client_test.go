package token

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClient_ReturnsEchoClient(t *testing.T) {
	t.Parallel()

	client := NewClient()

	assert.IsType(t, &EchoClient{}, client)
}

func TestEchoClient_CreateToken_ReturnsNil(t *testing.T) {
	t.Parallel()

	client := &EchoClient{}
	err := client.CreateToken(context.Background(), "test-service")

	assert.NoError(t, err)
}
