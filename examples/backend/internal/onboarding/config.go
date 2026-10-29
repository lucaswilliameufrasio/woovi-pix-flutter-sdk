package onboarding

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config contains local-only credentials for the Woovi sandbox example.
type Config struct {
	AppID          string
	SessionToken   string
	IdempotencyKey string
}

// Load reads and validates a local onboarding file without logging its values.
func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open local sandbox config: %w", err)
	}
	defer func() { _ = file.Close() }()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 8192)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return Config{}, errors.New("local sandbox config contains an invalid line")
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if _, exists := values[key]; exists {
			return Config{}, fmt.Errorf("local sandbox config repeats %s", key)
		}
		if strings.HasPrefix(value, "\"") {
			decodedValue, err := strconv.Unquote(value)
			if err != nil {
				return Config{}, errors.New("local sandbox config contains an invalid quoted value")
			}
			value = decodedValue
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return Config{}, fmt.Errorf("read local sandbox config: %w", err)
	}
	config := Config{
		AppID:          values["WOOVI_APP_ID"],
		SessionToken:   values["SANDBOX_SESSION_TOKEN"],
		IdempotencyKey: values["SANDBOX_IDEMPOTENCY_KEY"],
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Save creates the local onboarding file with restrictive permissions where supported.
func Save(path string, config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create local config directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create local sandbox config: %w", err)
	}
	content := strings.Join([]string{
		"# Local Woovi sandbox credentials. Never commit this file.",
		"WOOVI_APP_ID=" + strconv.Quote(config.AppID),
		"SANDBOX_SESSION_TOKEN=" + strconv.Quote(config.SessionToken),
		"SANDBOX_IDEMPOTENCY_KEY=" + strconv.Quote(config.IdempotencyKey),
		"",
	}, "\n")
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write local sandbox config: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close local sandbox config: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("restrict local sandbox config permissions: %w", err)
	}
	return nil
}

// Validate rejects incomplete credentials before any local service is started.
func (config Config) Validate() error {
	if strings.TrimSpace(config.AppID) == "" || len(config.AppID) > 1024 {
		return errors.New("sandbox AppID is required")
	}
	if len(config.SessionToken) < 32 {
		return errors.New("sandbox session token must contain at least 32 characters")
	}
	if len(config.IdempotencyKey) < 8 || len(config.IdempotencyKey) > 128 {
		return errors.New("sandbox idempotency key must contain between 8 and 128 characters")
	}
	return nil
}
