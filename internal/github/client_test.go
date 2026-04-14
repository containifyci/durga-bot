package github

import (
	"testing"

	"github.com/containifyci/durga-bot/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstallationClient_InvalidKey(t *testing.T) {
	t.Parallel()

	client, err := NewInstallationClient(1, 2, []byte("not-a-valid-pem"))

	assert.Nil(t, client)
	assert.ErrorContains(t, err, "creating github app installation transport")
}

func TestNewInstallationClient_ValidKey(t *testing.T) {
	t.Parallel()

	pemKey := testutil.GenerateRSAKey(t)
	client, err := NewInstallationClient(1, 2, pemKey)

	require.NoError(t, err)
	assert.NotNil(t, client)
}
