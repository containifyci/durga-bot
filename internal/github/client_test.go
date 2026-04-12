package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestRSAKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func TestNewInstallationClient_InvalidKey(t *testing.T) {
	t.Parallel()

	client, err := NewInstallationClient(1, 2, []byte("not-a-valid-pem"))

	assert.Nil(t, client)
	assert.ErrorContains(t, err, "creating github app installation transport")
}

func TestNewInstallationClient_ValidKey(t *testing.T) {
	t.Parallel()

	pemKey := generateTestRSAKey(t)
	client, err := NewInstallationClient(1, 2, pemKey)

	require.NoError(t, err)
	assert.NotNil(t, client)
}
