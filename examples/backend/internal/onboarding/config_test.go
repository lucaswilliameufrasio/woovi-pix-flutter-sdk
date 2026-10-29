package onboarding

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveAndLoadLocalConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".env")
	want := Config{
		AppID:          "sandbox app id with = and spaces",
		SessionToken:   "0123456789abcdef0123456789abcdef",
		IdempotencyKey: "0123456789abcdef0123456789abcdef",
	}
	if err := Save(configPath, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("loaded local config = %#v, want %#v", got, want)
	}
	fileInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fileInfo.Mode().Perm() != 0600 {
		t.Fatalf("local config mode = %04o, want 0600", fileInfo.Mode().Perm())
	}
}

func TestSaveDoesNotOverwriteExistingConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(configPath, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	config := Config{
		AppID:          "fixture-app-id",
		SessionToken:   "0123456789abcdef0123456789abcdef",
		IdempotencyKey: "0123456789abcdef",
	}
	if err := Save(configPath, config); err == nil {
		t.Fatal("Save unexpectedly overwrote an existing config")
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "preserve" {
		t.Fatalf("existing config changed to %q", content)
	}
}

func TestLoadRejectsDuplicateKeys(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".env")
	content := "WOOVI_APP_ID=one\nWOOVI_APP_ID=two\nSANDBOX_SESSION_TOKEN=0123456789abcdef0123456789abcdef\nSANDBOX_IDEMPOTENCY_KEY=0123456789abcdef\n"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath); err == nil {
		t.Fatal("Load accepted duplicate credential keys")
	}
}
