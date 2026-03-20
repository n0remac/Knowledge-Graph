package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldManageQdrantDocker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		baseURL string
		want    bool
	}{
		{baseURL: "http://localhost:6333", want: true},
		{baseURL: "http://127.0.0.1:6333/", want: true},
		{baseURL: "http://[::1]:6333", want: true},
		{baseURL: "http://localhost:7333", want: false},
		{baseURL: "https://localhost:6333", want: false},
		{baseURL: "http://qdrant.internal:6333", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.baseURL, func(t *testing.T) {
			t.Parallel()
			if got := shouldManageQdrantDocker(tc.baseURL); got != tc.want {
				t.Fatalf("shouldManageQdrantDocker(%q) = %v, want %v", tc.baseURL, got, tc.want)
			}
		})
	}
}

func TestEnsureQdrantDockerWithDepsStartsExistingContainer(t *testing.T) {
	t.Parallel()

	server := newQdrantReadyServer(t)
	runner := &fakeDockerRunner{
		inspectOutput: "qdrant/qdrant|false\n",
		execResults:   []fakeDockerResult{{}},
	}

	storageDir := filepath.Join(t.TempDir(), "qdrant_storage")
	if err := ensureQdrantDockerWithDeps(context.Background(), server.URL, storageDir, runner.Run, server.Client()); err != nil {
		t.Fatalf("ensureQdrantDockerWithDeps() error = %v", err)
	}

	if got := runner.commands(); len(got) != 3 {
		t.Fatalf("command count = %d, want 3 (%v)", len(got), got)
	}
	if !runner.calledWith("docker", "start", qdrantContainerName) {
		t.Fatalf("expected docker start command, got %v", runner.commands())
	}
	if runner.calledWith("docker", "run", "-d") {
		t.Fatalf("did not expect docker run command, got %v", runner.commands())
	}
	if !runner.calledWith("docker", "exec", qdrantContainerName) {
		t.Fatalf("expected docker exec mount validation, got %v", runner.commands())
	}
}

func TestEnsureQdrantDockerWithDepsRunsContainerWhenMissing(t *testing.T) {
	t.Parallel()

	server := newQdrantReadyServer(t)
	runner := &fakeDockerRunner{
		inspectErr:    errors.New("exit status 1"),
		inspectOutput: "Error: No such object: qdrant\n",
		execResults:   []fakeDockerResult{{}},
	}

	storageDir := filepath.Join(t.TempDir(), "qdrant_storage")
	if err := ensureQdrantDockerWithDeps(context.Background(), server.URL, storageDir, runner.Run, server.Client()); err != nil {
		t.Fatalf("ensureQdrantDockerWithDeps() error = %v", err)
	}

	if !runner.calledWith("docker", "run", "-d") {
		t.Fatalf("expected docker run command, got %v", runner.commands())
	}
	if !runner.lastRunContains("-v", storageDir+":/qdrant/storage") {
		t.Fatalf("expected docker run volume mount for %q, got %v", storageDir, runner.commands())
	}
	if !runner.calledWith("docker", "exec", qdrantContainerName) {
		t.Fatalf("expected docker exec mount validation, got %v", runner.commands())
	}
}

func TestEnsureQdrantDockerWithDepsRejectsWrongImage(t *testing.T) {
	t.Parallel()

	server := newQdrantReadyServer(t)
	runner := &fakeDockerRunner{
		inspectOutput: "redis:7|true\n",
	}

	err := ensureQdrantDockerWithDeps(context.Background(), server.URL, filepath.Join(t.TempDir(), "qdrant_storage"), runner.Run, server.Client())
	if err == nil {
		t.Fatal("ensureQdrantDockerWithDeps() error = nil, want wrong image error")
	}
	if !strings.Contains(err.Error(), `uses image "redis:7"`) {
		t.Fatalf("expected wrong image error, got %v", err)
	}
}

func TestEnsureQdrantDockerWithDepsRecreatesContainerWhenMountProbeFails(t *testing.T) {
	t.Parallel()

	server := newQdrantReadyServer(t)
	runner := &fakeDockerRunner{
		inspectOutput: "qdrant/qdrant|true\n",
		execResults: []fakeDockerResult{
			{output: "missing probe\n", err: errors.New("exit status 1")},
			{},
		},
	}

	storageDir := filepath.Join(t.TempDir(), "qdrant_storage")
	if err := ensureQdrantDockerWithDeps(context.Background(), server.URL, storageDir, runner.Run, server.Client()); err != nil {
		t.Fatalf("ensureQdrantDockerWithDeps() error = %v", err)
	}

	if !runner.calledWith("docker", "rm", "-f", qdrantContainerName) {
		t.Fatalf("expected docker rm -f command, got %v", runner.commands())
	}
	if !runner.calledWith("docker", "run", "-d") {
		t.Fatalf("expected docker run command after failed probe, got %v", runner.commands())
	}
	if runner.execCalls != 2 {
		t.Fatalf("exec call count = %d, want 2", runner.execCalls)
	}
}

type fakeDockerRunner struct {
	calls         [][]string
	inspectOutput string
	inspectErr    error
	startOutput   string
	startErr      error
	runOutput     string
	runErr        error
	removeOutput  string
	removeErr     error
	execResults   []fakeDockerResult
	execCalls     int
}

type fakeDockerResult struct {
	output string
	err    error
}

func (f *fakeDockerRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)

	if name != "docker" || len(args) == 0 {
		return nil, errors.New("unexpected command")
	}

	switch args[0] {
	case "inspect":
		return []byte(f.inspectOutput), f.inspectErr
	case "start":
		if f.startOutput == "" {
			return []byte(qdrantContainerName + "\n"), f.startErr
		}
		return []byte(f.startOutput), f.startErr
	case "run":
		if f.runOutput == "" {
			return []byte("container-id\n"), f.runErr
		}
		return []byte(f.runOutput), f.runErr
	case "rm":
		if f.removeOutput == "" {
			return []byte(qdrantContainerName + "\n"), f.removeErr
		}
		return []byte(f.removeOutput), f.removeErr
	case "exec":
		if f.execCalls < len(f.execResults) {
			result := f.execResults[f.execCalls]
			f.execCalls++
			return []byte(result.output), result.err
		}
		f.execCalls++
		return nil, nil
	default:
		return nil, errors.New("unexpected docker subcommand")
	}
}

func (f *fakeDockerRunner) commands() [][]string {
	return append([][]string(nil), f.calls...)
}

func (f *fakeDockerRunner) calledWith(parts ...string) bool {
	for _, call := range f.calls {
		if len(call) < len(parts) {
			continue
		}
		match := true
		for i := range parts {
			if call[i] != parts[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func (f *fakeDockerRunner) lastRunContains(flag, value string) bool {
	for i := len(f.calls) - 1; i >= 0; i-- {
		call := f.calls[i]
		if len(call) < 2 || call[0] != "docker" || call[1] != "run" {
			continue
		}
		for j := 0; j < len(call)-1; j++ {
			if call[j] == flag && call[j+1] == value {
				return true
			}
		}
	}
	return false
}

func newQdrantReadyServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != qdrantReadyPath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)
	return server
}
