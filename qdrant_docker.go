package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	qdrantContainerName  = "qdrant"
	qdrantImage          = "qdrant/qdrant"
	qdrantStorageDir     = "qdrant_storage"
	qdrantCollectionsDir = "collections"
	qdrantProbeFileName  = ".knowledgegraph-qdrant-probe"
	qdrantReadyPath      = "/collections"
	qdrantReadyTimeout   = 15 * time.Second
	qdrantPollInterval   = 300 * time.Millisecond
)

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type qdrantContainerState struct {
	Exists  bool
	Image   string
	Running bool
}

func ensureQdrantDocker(ctx context.Context, baseURL string) error {
	if !shouldManageQdrantDocker(baseURL) {
		return nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory for qdrant storage: %w", err)
	}

	storageDir := filepath.Join(workingDir, qdrantStorageDir)
	client := &http.Client{Timeout: 2 * time.Second}
	return ensureQdrantDockerWithDeps(ctx, strings.TrimRight(strings.TrimSpace(baseURL), "/"), storageDir, runCommand, client)
}

func ensureQdrantDockerWithDeps(ctx context.Context, baseURL, storageDir string, runner commandRunner, client *http.Client) error {
	if runner == nil {
		return fmt.Errorf("qdrant docker runner is not configured")
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return fmt.Errorf("create qdrant storage directory %q: %w", storageDir, err)
	}
	if err := os.MkdirAll(filepath.Join(storageDir, qdrantCollectionsDir), 0o755); err != nil {
		return fmt.Errorf("create qdrant collections directory %q: %w", filepath.Join(storageDir, qdrantCollectionsDir), err)
	}
	if err := writeQdrantStorageProbe(storageDir); err != nil {
		return err
	}

	state, err := inspectQdrantContainer(ctx, runner)
	if err != nil {
		return err
	}

	switch {
	case !state.Exists:
		if err := runQdrantContainer(ctx, storageDir, runner); err != nil {
			return err
		}
	case !strings.EqualFold(strings.TrimSpace(state.Image), qdrantImage):
		return fmt.Errorf("docker container %q uses image %q, want %q", qdrantContainerName, state.Image, qdrantImage)
	case !state.Running:
		if _, err := runner(ctx, "docker", "start", qdrantContainerName); err != nil {
			return fmt.Errorf("start existing qdrant docker container: %w", err)
		}
	}

	if err := waitForQdrantReady(ctx, client, baseURL); err != nil {
		return err
	}
	if err := validateQdrantStorageMount(ctx, runner); err != nil {
		if !state.Exists {
			return err
		}
		if _, removeErr := runner(ctx, "docker", "rm", "-f", qdrantContainerName); removeErr != nil {
			return fmt.Errorf("qdrant storage mount validation failed (%v) and remove container failed: %w", err, removeErr)
		}
		if err := runQdrantContainer(ctx, storageDir, runner); err != nil {
			return err
		}
		if err := waitForQdrantReady(ctx, client, baseURL); err != nil {
			return err
		}
		if err := validateQdrantStorageMount(ctx, runner); err != nil {
			return err
		}
	}
	return nil
}

func writeQdrantStorageProbe(storageDir string) error {
	probePath := filepath.Join(storageDir, qdrantProbeFileName)
	if err := os.WriteFile(probePath, []byte("ok\n"), 0o644); err != nil {
		return fmt.Errorf("write qdrant storage probe %q: %w", probePath, err)
	}
	return nil
}

func runQdrantContainer(ctx context.Context, storageDir string, runner commandRunner) error {
	if _, err := runner(ctx, "docker",
		"run", "-d",
		"--name", qdrantContainerName,
		"-p", "6333:6333",
		"-p", "6334:6334",
		"-v", storageDir+":/qdrant/storage",
		qdrantImage,
	); err != nil {
		return fmt.Errorf("start qdrant docker container: %w", err)
	}
	return nil
}

func validateQdrantStorageMount(ctx context.Context, runner commandRunner) error {
	output, err := runner(ctx, "docker", "exec", qdrantContainerName, "sh", "-lc", "test -f /qdrant/storage/"+qdrantProbeFileName)
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		return fmt.Errorf("validate qdrant storage mount: %w", wrapCommandError(err, trimmed))
	}
	return nil
}

func shouldManageQdrantDocker(baseURL string) bool {
	baseURL = strings.TrimSpace(baseURL)
	switch strings.TrimRight(baseURL, "/") {
	case "http://localhost:6333", "http://127.0.0.1:6333", "http://[::1]:6333":
		return true
	default:
		return false
	}
}

func inspectQdrantContainer(ctx context.Context, runner commandRunner) (qdrantContainerState, error) {
	output, err := runner(ctx, "docker", "inspect", "--format", "{{.Config.Image}}|{{.State.Running}}", qdrantContainerName)
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if isMissingContainerOutput(trimmed) {
			return qdrantContainerState{}, nil
		}
		return qdrantContainerState{}, fmt.Errorf("inspect qdrant docker container: %w", wrapCommandError(err, trimmed))
	}
	parts := strings.SplitN(trimmed, "|", 2)
	if len(parts) != 2 {
		return qdrantContainerState{}, fmt.Errorf("inspect qdrant docker container: unexpected output %q", trimmed)
	}
	return qdrantContainerState{
		Exists:  true,
		Image:   strings.TrimSpace(parts[0]),
		Running: strings.EqualFold(strings.TrimSpace(parts[1]), "true"),
	}, nil
}

func isMissingContainerOutput(output string) bool {
	output = strings.ToLower(strings.TrimSpace(output))
	return strings.Contains(output, "no such object") || strings.Contains(output, "no such container")
}

func waitForQdrantReady(ctx context.Context, client *http.Client, baseURL string) error {
	readyCtx, cancel := context.WithTimeout(ctx, qdrantReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(qdrantPollInterval)
	defer ticker.Stop()

	url := strings.TrimRight(strings.TrimSpace(baseURL), "/") + qdrantReadyPath
	var lastErr error
	for {
		lastErr = qdrantReadyRequest(readyCtx, client, url)
		if lastErr == nil {
			return nil
		}

		select {
		case <-readyCtx.Done():
			return fmt.Errorf("wait for qdrant at %s: %w", url, lastErr)
		case <-ticker.C:
		}
	}
}

func qdrantReadyRequest(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build readiness request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
	if readErr != nil {
		return fmt.Errorf("status %d and read body failed: %w", resp.StatusCode, readErr)
	}
	return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func wrapCommandError(err error, output string) error {
	if strings.TrimSpace(output) == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, output)
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
