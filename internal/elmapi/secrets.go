package elmapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/crypto/nacl/box"
)

const migratorSecretApp = "elm-exporter"

type secretPublicKey struct {
	ID  string `json:"key_id"`
	Key string `json:"key"`
}

type secretPayload struct {
	EncryptedValue string `json:"encrypted_value"`
	KeyID          string `json:"key_id"`
}

// SetMigratorSecret encrypts and stores one of the PATs used by the migrator.
// The API follows the organization Actions secrets protocol, with elm-exporter
// as its fixed application name.
func (c *Client) SetMigratorSecret(ctx context.Context, org, name, value string) error {
	if org == "" {
		return errors.New("organization must not be empty")
	}
	if value == "" {
		return errors.New("secret value must not be empty")
	}

	secretsPath := "/orgs/" + url.PathEscape(org) + "/" + migratorSecretApp + "/secrets"
	var publicKey secretPublicKey
	if err := c.get(ctx, secretsPath+"/public-key", nil, &publicKey); err != nil {
		return fmt.Errorf("failed to fetch public key: %w", err)
	}

	encryptedValue, err := sealSecret(value, publicKey.Key)
	if err != nil {
		return err
	}

	body := secretPayload{EncryptedValue: encryptedValue, KeyID: publicKey.ID}
	path := secretsPath + "/" + url.PathEscape(name)
	if err := c.sendJSON(ctx, http.MethodPut, path, body, nil, http.StatusCreated, http.StatusNoContent); err != nil {
		return fmt.Errorf("setting migrator secret %s: %w", name, err)
	}
	return nil
}

func sealSecret(value, encodedPublicKey string) (string, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(encodedPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode public key: %w", err)
	}
	if len(keyBytes) != 32 {
		return "", fmt.Errorf("invalid migrator secret public key length: got %d bytes, want 32", len(keyBytes))
	}

	var publicKey [32]byte
	copy(publicKey[:], keyBytes)
	encrypted, err := box.SealAnonymous(nil, []byte(value), &publicKey, nil)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt body: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}
