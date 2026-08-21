package elmapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetMigratorSecret(t *testing.T) {
	publicKey, privateKey, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, "Bearer admin-token", r.Header.Get("Authorization"))
		assert.Equal(t, apiVersion, r.Header.Get(apiVersionHeader))

		switch requests {
		case 1:
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/orgs/octo-org/elm-exporter/secrets/public-key", r.URL.Path)
			require.NoError(t, json.NewEncoder(w).Encode(secretPublicKey{
				ID:  "key-1",
				Key: base64.StdEncoding.EncodeToString(publicKey[:]),
			}))
		case 2:
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/orgs/octo-org/elm-exporter/secrets/SOURCE_PAT", r.URL.Path)

			var body secretPayload
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "key-1", body.KeyID)
			ciphertext, decodeErr := base64.StdEncoding.DecodeString(body.EncryptedValue)
			require.NoError(t, decodeErr)
			plaintext, ok := box.OpenAnonymous(nil, ciphertext, publicKey, privateKey)
			require.True(t, ok)
			assert.Equal(t, "pat-value", string(plaintext))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin-token", WithHTTPClient(server.Client()))
	require.NoError(t, client.SetMigratorSecret(t.Context(), "octo-org", "SOURCE_PAT", "pat-value"))
	assert.Equal(t, 2, requests)
}

func TestSealSecretRejectsInvalidPublicKey(t *testing.T) {
	_, err := sealSecret("pat-value", base64.StdEncoding.EncodeToString([]byte("short")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "got 5 bytes, want 32")
}
