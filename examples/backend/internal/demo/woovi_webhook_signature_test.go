package demo

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWooviWebhookSignatureUsesRawBodyAndKeyRotationSet(t *testing.T) {
	oldKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_ = json.NewEncoder(w).Encode(map[string]any{"public_keys": []map[string]string{
			{"key": encodePublicKey(&oldKey.PublicKey)},
			{"key": encodePublicKey(&newKey.PublicKey)},
		}})
	}))
	defer server.Close()
	verifier := NewWooviSignatureVerifier(server.Client(), server.URL)
	raw := []byte("{ \"charge\" : {\"status\":\"COMPLETED\"}, \"event\":\"OPENPIX:CHARGE_COMPLETED\"}\n")
	signature := signWebhook(t, newKey, raw)
	valid, err := verifier.Verify(context.Background(), raw, signature)
	if err != nil || !valid {
		t.Fatalf("valid current key rejected: valid=%v err=%v", valid, err)
	}
	valid, err = verifier.Verify(context.Background(), []byte(`{"event":"OPENPIX:CHARGE_COMPLETED","charge":{"status":"COMPLETED"}}`), signature)
	if err != nil || valid {
		t.Fatalf("re-serialized body must fail signature: valid=%v err=%v", valid, err)
	}
	valid, err = verifier.Verify(context.Background(), raw, "not-base64")
	if err != nil || valid {
		t.Fatalf("invalid base64 must fail closed: valid=%v err=%v", valid, err)
	}
}

func TestWooviVerifierUsesCachedKeysWhenRefreshFails(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var now = time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"public_keys": []map[string]string{{"key": encodePublicKey(&key.PublicKey)}}})
	}))
	verifier := NewWooviSignatureVerifier(server.Client(), server.URL)
	verifier.now = func() time.Time { return now }
	raw := []byte(`{"event":"test"}`)
	if ok, err := verifier.Verify(context.Background(), raw, signWebhook(t, key, raw)); err != nil || !ok {
		t.Fatalf("initial signature verification: ok=%v err=%v", ok, err)
	}
	server.Close()
	now = now.Add(2 * time.Hour)
	if ok, err := verifier.Verify(context.Background(), raw, signWebhook(t, key, raw)); err != nil || !ok {
		t.Fatalf("cached key must cover endpoint outage: ok=%v err=%v", ok, err)
	}
}

func encodePublicKey(key *rsa.PublicKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: mustMarshalPublicKey(key)}))
}

func mustMarshalPublicKey(key *rsa.PublicKey) []byte {
	encoded, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		panic(err)
	}
	return encoded
}

func signWebhook(t *testing.T, key *rsa.PrivateKey, raw []byte) string {
	t.Helper()
	digest := sha256.Sum256(raw)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(signature)
}
