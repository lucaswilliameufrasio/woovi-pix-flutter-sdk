package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseRunOptions(t *testing.T) {
	options, err := parseRunOptions([]string{"--platform", "ios", "--device-id", "simulator-1", "--new-round"})
	if err != nil {
		t.Fatal(err)
	}
	want := runOptions{deviceID: "simulator-1", newRound: true, platform: "ios"}
	if options != want {
		t.Fatalf("run options = %#v, want %#v", options, want)
	}
}

func TestParseRunOptionsRejectsUnsupportedPlatform(t *testing.T) {
	if _, err := parseRunOptions([]string{"--platform", "web"}); err == nil {
		t.Fatal("parseRunOptions accepted an unsupported platform")
	}
}

func TestParseRunOptionsRejectsNewRoundAndResumeTogether(t *testing.T) {
	if _, err := parseRunOptions([]string{"--new-round", "--resume"}); err == nil {
		t.Fatal("parseRunOptions accepted incompatible recovery options")
	}
}

func TestMergeEnvironmentReplacesOverrides(t *testing.T) {
	got := mergeEnvironment([]string{"PATH=/bin", "WOOVI_APP_ID=old", "DEMO_ADDR=old"}, map[string]string{
		"WOOVI_APP_ID": "new",
		"DEMO_ADDR":    "127.0.0.1:8080",
	})
	want := []string{"PATH=/bin", "WOOVI_APP_ID=new", "DEMO_ADDR=127.0.0.1:8080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged environment = %#v, want %#v", got, want)
	}
}

func TestWriteDartDefinesUsesRestrictedTemporaryFile(t *testing.T) {
	projectRoot := t.TempDir()
	options := runOptions{platform: "android"}
	definesPath, err := writeDartDefines(projectRoot, "session-token-fixture", "key-fixture", "sandbox-order-fixture", options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(definesPath)
	})
	fileInfo, err := os.Stat(definesPath)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0600 {
		t.Fatalf("Flutter defines mode = %04o, want 0600", fileInfo.Mode().Perm())
	}
	content, err := os.ReadFile(definesPath)
	if err != nil {
		t.Fatal(err)
	}
	var definitions map[string]string
	if err := json.Unmarshal(content, &definitions); err != nil {
		t.Fatal(err)
	}
	if definitions["SANDBOX_SESSION_TOKEN"] != "session-token-fixture" ||
		definitions["SANDBOX_IDEMPOTENCY_KEY"] != "key-fixture" ||
		definitions["SANDBOX_ORDER_ID"] != "sandbox-order-fixture" ||
		definitions["DEMO_BACKEND"] != "http://10.0.2.2:8080" {
		t.Fatalf("unexpected Flutter defines: %#v", definitions)
	}
	if _, exists := definitions["WOOVI_APP_ID"]; exists {
		t.Fatal("Woovi AppID must not be passed to the Flutter app")
	}
	if filepath.Dir(filepath.Dir(definesPath)) != filepath.Join(projectRoot, "example") {
		t.Fatalf("Flutter defines were not written under the example tooling directory: %s", definesPath)
	}
}
