package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/woovi-pix-flutter-sdk/examples/backend/internal/onboarding"
	"golang.org/x/term"
)

const (
	backendAddress = "127.0.0.1:8080"
	composeProject = "woovi-pix-flutter-sdk-cli"
)

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(arguments []string) error {
	if len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h" || arguments[0] == "help") {
		printUsage(os.Stdout)
		return nil
	}
	projectRoot, err := findProjectRoot()
	if err != nil {
		return err
	}
	backendRoot := filepath.Join(projectRoot, "examples", "backend")
	configPath := filepath.Join(backendRoot, ".env")
	command := "run"
	if len(arguments) > 0 && !strings.HasPrefix(arguments[0], "-") {
		command = arguments[0]
		arguments = arguments[1:]
	}
	switch command {
	case "setup":
		if len(arguments) != 0 {
			return errors.New("usage: woovi-demo setup")
		}
		return setup(configPath)
	case "run":
		for _, argument := range arguments {
			if argument == "--help" || argument == "-h" {
				printUsage(os.Stdout)
				return nil
			}
		}
		return run(projectRoot, backendRoot, configPath, arguments)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", command)
	}
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Woovi sandbox demo")
	fmt.Fprintln(writer, "  woovi-demo setup  Configure local sandbox credentials")
	fmt.Fprintln(writer, "  woovi-demo run    Start PostgreSQL, backend, and Flutter")
	fmt.Fprintln(writer, "  woovi-demo run --platform ios --device-id <id>")
	fmt.Fprintln(writer, "  woovi-demo run --new-round  Start a fresh test order after a terminal round")
	fmt.Fprintln(writer, "  woovi-demo run --resume     Recover an exact order/key shown by the app")
}

func findProjectRoot() (string, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}
	for directory := workingDirectory; ; directory = filepath.Dir(directory) {
		flutterExample := filepath.Join(directory, "example", "pubspec.yaml")
		backendCompose := filepath.Join(directory, "examples", "backend", "docker-compose.yml")
		if regularFileExists(flutterExample) && regularFileExists(backendCompose) {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return "", errors.New("run woovi-demo from inside the woovi-pix-flutter-sdk checkout")
}

func regularFileExists(path string) bool {
	fileInfo, err := os.Stat(path)
	return err == nil && fileInfo.Mode().IsRegular()
}

func randomHex(byteCount int) (string, error) {
	randomBytes := make([]byte, byteCount)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

func setup(configPath string) error {
	if _, err := os.Lstat(configPath); err == nil {
		return errors.New("sandbox config already exists; keep it to recover uncertain attempts, or remove it yourself before setup")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect local sandbox config: %w", err)
	}
	fmt.Fprint(os.Stdout, "Woovi sandbox AppID (input hidden): ")
	appIDBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return errors.New("could not read the AppID securely; run setup from an interactive terminal")
	}
	appID := strings.TrimSpace(string(appIDBytes))
	clear(appIDBytes)
	if appID == "" {
		return errors.New("sandbox AppID cannot be empty")
	}
	sessionToken, err := randomHex(32)
	if err != nil {
		return fmt.Errorf("generate sandbox session token: %w", err)
	}
	idempotencyKey, err := randomHex(16)
	if err != nil {
		return fmt.Errorf("generate sandbox idempotency key: %w", err)
	}
	config := onboarding.Config{
		AppID:          appID,
		SessionToken:   sessionToken,
		IdempotencyKey: idempotencyKey,
	}
	if err := onboarding.Save(configPath, config); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Sandbox config saved locally (0600 permissions on macOS/Linux).")
	fmt.Fprintln(os.Stdout, "Run `mise exec -- go run ./cmd/woovi-demo` to start the example.")
	return nil
}

func run(projectRoot, backendRoot, configPath string, arguments []string) error {
	options, err := parseRunOptions(arguments)
	if err != nil {
		return err
	}
	config, err := onboarding.Load(configPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := setup(configPath); err != nil {
			return err
		}
		config, err = onboarding.Load(configPath)
	}
	if err != nil {
		return err
	}
	if err := requireCommands("docker", "go", "flutter"); err != nil {
		return err
	}
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	dockerCompose := []string{"compose", "-f", "docker-compose.yml", "-p", composeProject}
	if err := runCommand(ctx, backendRoot, os.Stdout, os.Stderr, "docker", append(dockerCompose, "up", "-d", "--wait", "postgres")...); err != nil {
		return fmt.Errorf("start local PostgreSQL: %w", err)
	}
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = runCommand(stopContext, backendRoot, os.Stdout, os.Stderr, "docker", append(dockerCompose, "stop", "postgres")...)
	}()
	portOutput, err := commandOutput(ctx, backendRoot, "docker", append(dockerCompose, "port", "postgres", "5432")...)
	if err != nil {
		return fmt.Errorf("find local PostgreSQL port: %w", err)
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(portOutput))
	if err != nil {
		return errors.New("Docker Compose returned an invalid PostgreSQL port")
	}
	databaseURL := "postgres://woovi_demo:woovi_demo@127.0.0.1:" + port + "/woovi_demo?sslmode=disable"
	serverBinary, cleanupBuild, err := buildBackend(ctx, backendRoot)
	if err != nil {
		return err
	}
	defer cleanupBuild()
	serverCommand := exec.Command(serverBinary)
	serverCommand.Dir = backendRoot
	serverCommand.Env = mergeEnvironment(os.Environ(), map[string]string{
		"DEMO_ADDR":                     backendAddress,
		"DATABASE_URL":                  databaseURL,
		"ENABLE_DEMO_PSP":               "false",
		"ENABLE_WOOVI_RECONCILIATION":   "false",
		"ENABLE_WOOVI_WEBHOOK":          "false",
		"ENABLE_WOOVI_SANDBOX_CHECKOUT": "true",
		"SANDBOX_SESSION_TOKEN":         config.SessionToken,
		"WOOVI_API_BASE_URL":            "https://api.woovi-sandbox.com",
		"WOOVI_APP_ID":                  config.AppID,
	})
	serverCommand.Stdout = os.Stdout
	serverCommand.Stderr = os.Stderr
	if err := serverCommand.Start(); err != nil {
		return fmt.Errorf("start local backend: %w", err)
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serverCommand.Wait()
	}()
	if err := waitForBackend(ctx, serverDone); err != nil {
		stopProcess(serverCommand.Process, serverDone)
		return err
	}
	orderID := "sandbox-order-1"
	idempotencyKey := config.IdempotencyKey
	if options.newRound {
		orderID, err = createTestOrder(ctx, config.SessionToken)
		if err != nil {
			stopProcess(serverCommand.Process, serverDone)
			return err
		}
		idempotencyKey, err = randomHex(16)
		if err != nil {
			stopProcess(serverCommand.Process, serverDone)
			return fmt.Errorf("generate round idempotency key: %w", err)
		}
	}
	if options.resume {
		orderID, idempotencyKey, err = readRecoveryDetails()
		if err != nil {
			stopProcess(serverCommand.Process, serverDone)
			return err
		}
	}
	definesPath, err := writeDartDefines(projectRoot, config.SessionToken, idempotencyKey, orderID, options)
	if err != nil {
		stopProcess(serverCommand.Process, serverDone)
		return err
	}
	defer func() { _ = os.Remove(definesPath) }()
	fmt.Fprintln(os.Stdout, "Local services are ready. Starting Flutter; Ctrl+C stops the app, backend, and PostgreSQL (database data is preserved).")
	flutterArguments := []string{"run", "--dart-define-from-file=" + definesPath}
	if options.deviceID != "" {
		flutterArguments = append(flutterArguments, "--device-id", options.deviceID)
	}
	flutterCommand := exec.Command("flutter", flutterArguments...)
	flutterCommand.Dir = filepath.Join(projectRoot, "example")
	flutterCommand.Env = os.Environ()
	flutterCommand.Stdin = os.Stdin
	flutterCommand.Stdout = os.Stdout
	flutterCommand.Stderr = os.Stderr
	if err := flutterCommand.Start(); err != nil {
		stopProcess(serverCommand.Process, serverDone)
		return fmt.Errorf("start Flutter example: %w", err)
	}
	flutterDone := make(chan error, 1)
	go func() {
		flutterDone <- flutterCommand.Wait()
	}()
	select {
	case err := <-flutterDone:
		stopProcess(serverCommand.Process, serverDone)
		if err != nil {
			return fmt.Errorf("Flutter example stopped: %w", err)
		}
		return nil
	case err := <-serverDone:
		_ = flutterCommand.Process.Kill()
		<-flutterDone
		if err != nil {
			return fmt.Errorf("local backend stopped unexpectedly: %w", err)
		}
		return errors.New("local backend stopped unexpectedly")
	case <-ctx.Done():
		stopProcess(flutterCommand.Process, flutterDone)
		stopProcess(serverCommand.Process, serverDone)
		return nil
	}
}

type runOptions struct {
	deviceID string
	resume   bool
	newRound bool
	platform string
}

func parseRunOptions(arguments []string) (runOptions, error) {
	flagSet := flag.NewFlagSet("run", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	options := runOptions{}
	flagSet.StringVar(&options.deviceID, "device-id", "", "Flutter device identifier")
	flagSet.BoolVar(&options.newRound, "new-round", false, "create a fresh sandbox order before launching Flutter")
	flagSet.BoolVar(&options.resume, "resume", false, "resume an exact order and idempotency key shown by the app")
	flagSet.StringVar(&options.platform, "platform", "android", "target platform: android or ios")
	if err := flagSet.Parse(arguments); err != nil {
		return runOptions{}, fmt.Errorf("parse run options: %w", err)
	}
	if flagSet.NArg() != 0 {
		return runOptions{}, errors.New("run accepts only --device-id, --new-round, and --platform")
	}
	if options.platform != "android" && options.platform != "ios" {
		return runOptions{}, errors.New("--platform must be android or ios")
	}
	if options.newRound && options.resume {
		return runOptions{}, errors.New("--new-round and --resume cannot be used together")
	}
	return options, nil
}

func readRecoveryDetails() (string, string, error) {
	fmt.Fprint(os.Stdout, "Order ID shown by the app (input hidden): ")
	orderIDBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", "", errors.New("could not read the order ID securely; run from an interactive terminal")
	}
	orderID := strings.TrimSpace(string(orderIDBytes))
	clear(orderIDBytes)
	fmt.Fprint(os.Stdout, "Idempotency key shown by the app (input hidden): ")
	idempotencyKeyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", "", errors.New("could not read the idempotency key securely; run from an interactive terminal")
	}
	idempotencyKey := strings.TrimSpace(string(idempotencyKeyBytes))
	clear(idempotencyKeyBytes)
	if orderID == "" || len(idempotencyKey) < 8 || len(idempotencyKey) > 128 {
		return "", "", errors.New("the order ID or idempotency key is invalid")
	}
	return orderID, idempotencyKey, nil
}

func requireCommands(commands ...string) error {
	for _, command := range commands {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("%s is required; install the project tools with mise and install Docker Desktop", command)
		}
	}
	return nil
}

func runCommand(ctx context.Context, directory string, stdout, stderr io.Writer, command string, arguments ...string) error {
	process := exec.CommandContext(ctx, command, arguments...)
	process.Dir = directory
	process.Stdout = stdout
	process.Stderr = stderr
	if err := process.Run(); err != nil {
		return fmt.Errorf("%s command failed: %w", command, err)
	}
	return nil
}

func commandOutput(ctx context.Context, directory, command string, arguments ...string) (string, error) {
	process := exec.CommandContext(ctx, command, arguments...)
	process.Dir = directory
	output, err := process.Output()
	if err != nil {
		return "", fmt.Errorf("%s command failed: %w", command, err)
	}
	return string(output), nil
}

func buildBackend(ctx context.Context, backendRoot string) (string, func(), error) {
	buildDirectory, err := os.MkdirTemp("", "woovi-pix-demo-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary build directory: %w", err)
	}
	serverBinary := filepath.Join(buildDirectory, "server")
	if filepath.Ext(os.Args[0]) == ".exe" {
		serverBinary += ".exe"
	}
	if err := runCommand(ctx, backendRoot, os.Stdout, os.Stderr, "go", "build", "-o", serverBinary, "./cmd/server"); err != nil {
		_ = os.RemoveAll(buildDirectory)
		return "", func() {}, fmt.Errorf("build local backend: %w", err)
	}
	return serverBinary, func() { _ = os.RemoveAll(buildDirectory) }, nil
}

func waitForBackend(ctx context.Context, serverDone chan error) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+backendAddress+"/healthz", nil)
		if err == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		select {
		case err := <-serverDone:
			serverDone <- err
			if err != nil {
				return fmt.Errorf("local backend exited before becoming ready: %w", err)
			}
			return errors.New("local backend exited before becoming ready")
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("local backend did not become ready within 20 seconds")
		case <-ticker.C:
		}
	}
}

func createTestOrder(ctx context.Context, sessionToken string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+backendAddress+"/v1/sandbox/test-orders", nil)
	if err != nil {
		return "", fmt.Errorf("build new sandbox order request: %w", err)
	}
	request.Header.Set("X-Sandbox-Session", sessionToken)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return "", fmt.Errorf("create new local sandbox order: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create new local sandbox order returned HTTP %d", response.StatusCode)
	}
	var result struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result); err != nil {
		return "", fmt.Errorf("read new local sandbox order: %w", err)
	}
	if result.OrderID == "" {
		return "", errors.New("local sandbox order response did not include an order ID")
	}
	return result.OrderID, nil
}

func writeDartDefines(projectRoot, sessionToken, idempotencyKey, orderID string, options runOptions) (string, error) {
	backendURL := "http://10.0.2.2:8080"
	if options.platform == "ios" {
		backendURL = "http://127.0.0.1:8080"
	}
	definitions := map[string]string{
		"WOOVI_SANDBOX":           "true",
		"SANDBOX_SESSION_TOKEN":   sessionToken,
		"SANDBOX_IDEMPOTENCY_KEY": idempotencyKey,
		"SANDBOX_ORDER_ID":        orderID,
		"DEMO_BACKEND":            backendURL,
	}
	content, err := json.Marshal(definitions)
	if err != nil {
		return "", fmt.Errorf("encode temporary Flutter configuration: %w", err)
	}
	toolingDirectory := filepath.Join(projectRoot, "example", ".dart_tool")
	if err := os.MkdirAll(toolingDirectory, 0700); err != nil {
		return "", fmt.Errorf("create temporary Flutter config directory: %w", err)
	}
	file, err := os.CreateTemp(toolingDirectory, "woovi-sandbox-defines-*.json")
	if err != nil {
		return "", fmt.Errorf("create temporary Flutter config: %w", err)
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("restrict temporary Flutter config permissions: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("write temporary Flutter config: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("close temporary Flutter config: %w", err)
	}
	return file.Name(), nil
}

func mergeEnvironment(environment []string, overrides map[string]string) []string {
	merged := make([]string, 0, len(environment)+len(overrides))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, overridden := overrides[key]; overridden {
				continue
			}
		}
		merged = append(merged, entry)
	}
	for key, value := range overrides {
		merged = append(merged, key+"="+value)
	}
	return merged
}

func stopProcess(process *os.Process, processDone <-chan error) {
	if process == nil {
		return
	}
	if err := process.Signal(os.Interrupt); err != nil {
		_ = process.Kill()
	}
	select {
	case <-processDone:
	case <-time.After(5 * time.Second):
		_ = process.Kill()
		<-processDone
	}
}
