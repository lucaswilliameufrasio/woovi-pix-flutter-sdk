package demo

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const defaultWooviPublicKeysURL = "https://api.woovi.com/api/v1/webhook/public-keys"

const maxPublicKeysResponse = 1 << 20

type WooviSignatureVerifier struct {
	client *http.Client
	url    string

	mu        sync.Mutex
	keys      []*rsa.PublicKey
	refreshed time.Time
	ttl       time.Duration
	now       func() time.Time
}

func NewWooviSignatureVerifier(client *http.Client, url string) *WooviSignatureVerifier {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if url == "" {
		url = defaultWooviPublicKeysURL
	}
	return &WooviSignatureVerifier{client: client, url: url, ttl: time.Hour, now: time.Now}
}

// Verify checks x-webhook-signature against every currently published Woovi RSA key.
// It receives the exact raw HTTP request bytes; callers must verify before JSON parsing.
func (v *WooviSignatureVerifier) Verify(ctx context.Context, rawBody []byte, encodedSignature string) (bool, error) {
	if len(rawBody) == 0 || len(rawBody) > 10<<20 || len(encodedSignature) == 0 || len(encodedSignature) > 8192 {
		return false, errors.New("invalid webhook signature input")
	}
	signature, err := base64.StdEncoding.DecodeString(encodedSignature)
	if err != nil {
		return false, nil
	}
	keys, err := v.publicKeys(ctx)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(rawBody)
	for _, key := range keys {
		if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil {
			return true, nil
		}
	}
	return false, nil
}

func (v *WooviSignatureVerifier) publicKeys(ctx context.Context) ([]*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.keys) > 0 && v.now().Sub(v.refreshed) < v.ttl {
		return v.keys, nil
	}
	keys, err := v.fetchKeys(ctx)
	if err != nil {
		if len(v.keys) > 0 {
			return v.keys, nil
		}
		return nil, err
	}
	v.keys = keys
	v.refreshed = v.now()
	return v.keys, nil
}

func (v *WooviSignatureVerifier) fetchKeys(ctx context.Context) ([]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build Woovi public key request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	response, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Woovi webhook keys: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch Woovi webhook keys: HTTP %d", response.StatusCode)
	}
	var body struct {
		PublicKeys []struct {
			Key string `json:"key"`
		} `json:"public_keys"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxPublicKeysResponse)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode Woovi webhook keys: %w", err)
	}
	if len(body.PublicKeys) == 0 || len(body.PublicKeys) > 32 {
		return nil, errors.New("woovi webhook key set is empty or too large")
	}
	keys := make([]*rsa.PublicKey, 0, len(body.PublicKeys))
	for _, entry := range body.PublicKeys {
		block, _ := pem.Decode([]byte(entry.Key))
		if block == nil || block.Type != "PUBLIC KEY" {
			return nil, errors.New("invalid PEM in Woovi webhook key set")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse Woovi webhook public key: %w", err)
		}
		key, ok := parsed.(*rsa.PublicKey)
		if !ok || key.Size() < 256 {
			return nil, errors.New("woovi webhook key must be RSA-2048 or stronger")
		}
		keys = append(keys, key)
	}
	return keys, nil
}
