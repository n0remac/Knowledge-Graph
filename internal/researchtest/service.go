package researchtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/research"
)

var ErrNotFound = errors.New("research run not found")

type ServiceConfig struct {
	BaseDir                string
	MemoryStorePath        string
	OllamaBaseURL          string
	RequestTimeout         time.Duration
	EmbeddingModel         string
	QdrantBaseURL          string
	QdrantAPIKey           string
	QdrantCollectionPrefix string
}

type Service struct {
	cfg      ServiceConfig
	store    *memory.Store
	registry *research.SourceRegistry
	planner  *research.RulePlanner
}

func NewService(cfg ServiceConfig) (*Service, error) {
	baseDir := filepath.Clean(strings.TrimSpace(cfg.BaseDir))
	if baseDir == "" || baseDir == "." {
		return nil, fmt.Errorf("research base dir cannot be empty")
	}

	memoryStorePath := filepath.Clean(strings.TrimSpace(cfg.MemoryStorePath))
	if memoryStorePath == "" || memoryStorePath == "." {
		return nil, fmt.Errorf("memory store path cannot be empty")
	}
	if _, err := os.Stat(memoryStorePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("memory store %q does not exist", memoryStorePath)
		}
		return nil, fmt.Errorf("stat memory store: %w", err)
	}

	cfg.BaseDir = baseDir
	cfg.MemoryStorePath = memoryStorePath
	cfg.OllamaBaseURL = strings.TrimRight(strings.TrimSpace(cfg.OllamaBaseURL), "/")
	cfg.EmbeddingModel = strings.TrimSpace(cfg.EmbeddingModel)
	cfg.QdrantBaseURL = strings.TrimRight(strings.TrimSpace(cfg.QdrantBaseURL), "/")
	cfg.QdrantAPIKey = strings.TrimSpace(cfg.QdrantAPIKey)
	cfg.QdrantCollectionPrefix = strings.TrimSpace(cfg.QdrantCollectionPrefix)
	if cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("research request timeout must be > 0")
	}
	if cfg.OllamaBaseURL == "" {
		return nil, fmt.Errorf("research ollama base url cannot be empty")
	}
	if cfg.EmbeddingModel == "" {
		return nil, fmt.Errorf("research embedding model cannot be empty")
	}
	if cfg.QdrantBaseURL == "" {
		return nil, fmt.Errorf("research qdrant base url cannot be empty")
	}
	if cfg.QdrantCollectionPrefix == "" {
		return nil, fmt.Errorf("research qdrant collection prefix cannot be empty")
	}
	if err := os.MkdirAll(filepath.Join(cfg.BaseDir, "runs"), 0o755); err != nil {
		return nil, fmt.Errorf("create research base dir: %w", err)
	}

	store, err := memory.NewStore(cfg.MemoryStorePath, nil)
	if err != nil {
		return nil, err
	}

	index, err := embedding.New(ollama.NewClient(cfg.OllamaBaseURL, cfg.RequestTimeout), embedding.Config{
		QdrantBaseURL:    cfg.QdrantBaseURL,
		QdrantAPIKey:     cfg.QdrantAPIKey,
		CollectionPrefix: cfg.QdrantCollectionPrefix,
		RequestTimeout:   cfg.RequestTimeout,
	})
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	registry := research.NewSourceRegistry()
	if err := registry.Register(research.NewMemorySource(store, index, cfg.EmbeddingModel)); err != nil {
		_ = store.Close()
		return nil, err
	}

	return &Service{
		cfg:      cfg,
		store:    store,
		registry: registry,
		planner:  research.NewRulePlanner("memory"),
	}, nil
}

func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *Service) Defaults() Defaults {
	if s == nil {
		return Defaults{}
	}
	return Defaults{EmbeddingModel: s.cfg.EmbeddingModel}
}

func (s *Service) RunQuery(ctx context.Context, request RunRequest) (RunDetail, error) {
	if s == nil || s.store == nil || s.registry == nil || s.planner == nil {
		return RunDetail{}, fmt.Errorf("research test service is not initialized")
	}

	normalized, err := s.prepareRunRequest(request)
	if err != nil {
		return RunDetail{}, err
	}

	runID, err := s.nextRunID()
	if err != nil {
		return RunDetail{}, err
	}

	record := RunRecord{
		ID:              sanitizeID(runID),
		Query:           normalized.Query,
		ConversationID:  normalized.ConversationID,
		TopK:            normalized.TopK,
		EmbeddingModel:  s.cfg.EmbeddingModel,
		Status:          "running",
		StartedAtUnixMs: time.Now().UTC().UnixMilli(),
	}
	if err := s.writeRunRecord(record); err != nil {
		return RunDetail{}, err
	}

	plan, err := s.planner.Plan(normalized)
	if err != nil {
		record = s.failRun(record, err)
		if writeErr := s.writeRunRecord(record); writeErr != nil {
			return RunDetail{}, writeErr
		}
		return RunDetail{}, err
	}
	record.Plan = plan
	if err := s.writeRunRecord(record); err != nil {
		return RunDetail{}, err
	}

	artifacts := make([]research.ResearchArtifact, 0, normalized.TopK)
	for _, sourceQuery := range plan.Queries {
		source, ok := s.registry.Searchable(sourceQuery.SourceName)
		if !ok {
			record = s.failRun(record, fmt.Errorf("searchable source %q not found", sourceQuery.SourceName))
			if writeErr := s.writeRunRecord(record); writeErr != nil {
				return RunDetail{}, writeErr
			}
			return RunDetail{}, fmt.Errorf("searchable source %q not found", sourceQuery.SourceName)
		}
		items, searchErr := source.Search(ctx, research.SearchRequest{
			Query:          sourceQuery.Query,
			ConversationID: sourceQuery.ConversationID,
			TopK:           sourceQuery.TopK,
		})
		if searchErr != nil {
			record = s.failRun(record, searchErr)
			if writeErr := s.writeRunRecord(record); writeErr != nil {
				return RunDetail{}, writeErr
			}
			return RunDetail{}, searchErr
		}
		artifacts = append(artifacts, items...)
	}

	contextResult := research.BuildResearchContext(normalized, artifacts)
	artifacts = applySelectionReasons(artifacts, contextResult.Artifacts)
	record.Status = "completed"
	record.CompletedAtUnixMs = time.Now().UTC().UnixMilli()
	record.ArtifactCount = len(artifacts)
	record.SelectedArtifactCount = len(contextResult.Artifacts)
	record.Context = contextResult
	record.Error = ""

	if err := s.writeRunArtifacts(record.ID, artifacts); err != nil {
		return RunDetail{}, err
	}
	if err := s.writeRunRecord(record); err != nil {
		return RunDetail{}, err
	}

	return RunDetail{
		Run:       record,
		Artifacts: artifacts,
	}, nil
}

func (s *Service) GetRun(id string) (RunDetail, error) {
	if s == nil {
		return RunDetail{}, fmt.Errorf("research test service is not initialized")
	}

	id = sanitizeID(id)
	if id == "" {
		return RunDetail{}, ErrNotFound
	}

	record, err := s.readRunRecord(s.runResultPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RunDetail{}, ErrNotFound
		}
		return RunDetail{}, err
	}

	artifacts, err := s.readRunArtifacts(s.runArtifactsPath(id))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return RunDetail{}, err
		}
	}

	return RunDetail{
		Run:       record,
		Artifacts: artifacts,
	}, nil
}

func (s *Service) ListRuns(limit int) ([]RunRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("research test service is not initialized")
	}
	if limit <= 0 {
		limit = 20
	}

	entries, err := os.ReadDir(s.runsDir())
	if err != nil {
		return nil, fmt.Errorf("read research runs dir: %w", err)
	}

	out := make([]RunRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		record, err := s.readRunRecord(filepath.Join(s.runsDir(), entry.Name(), "result.json"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		out = append(out, record)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAtUnixMs == out[j].StartedAtUnixMs {
			return out[i].ID > out[j].ID
		}
		return out[i].StartedAtUnixMs > out[j].StartedAtUnixMs
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Service) prepareRunRequest(request RunRequest) (research.SearchRequest, error) {
	return research.NormalizeSearchRequest(research.SearchRequest{
		Query:          request.Query,
		ConversationID: request.ConversationID,
		TopK:           request.TopK,
	})
}

func applySelectionReasons(artifacts, selected []research.ResearchArtifact) []research.ResearchArtifact {
	if len(artifacts) == 0 || len(selected) == 0 {
		return artifacts
	}

	reasons := make(map[string]string, len(selected))
	for _, artifact := range selected {
		reasons[artifact.ArtifactID] = artifact.Provenance.SelectionReason
	}

	out := make([]research.ResearchArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		item := artifact
		if reason, ok := reasons[item.ArtifactID]; ok {
			item.Provenance.SelectionReason = reason
		}
		out = append(out, item)
	}
	return out
}

func (s *Service) failRun(record RunRecord, err error) RunRecord {
	record.Status = "failed"
	record.CompletedAtUnixMs = time.Now().UTC().UnixMilli()
	if err != nil {
		record.Error = err.Error()
	}
	return record
}

func (s *Service) runsDir() string {
	return filepath.Join(s.cfg.BaseDir, "runs")
}

func (s *Service) runDir(id string) string {
	return filepath.Join(s.runsDir(), sanitizeID(id))
}

func (s *Service) runResultPath(id string) string {
	return filepath.Join(s.runDir(id), "result.json")
}

func (s *Service) runArtifactsPath(id string) string {
	return filepath.Join(s.runDir(id), "artifacts.json")
}

func (s *Service) nextRunID() (string, error) {
	stamp := time.Now().UTC().Format("20060102t150405.000z")
	for i := 0; i < 1000; i++ {
		candidate := "run-" + stamp
		if i > 0 {
			candidate = fmt.Sprintf("run-%s-%d", stamp, i+1)
		}
		if _, err := os.Stat(s.runDir(candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("check research run id: %w", err)
		}
	}
	return "", fmt.Errorf("unable to allocate research run id")
}

func (s *Service) writeRunRecord(record RunRecord) error {
	record.ID = sanitizeID(record.ID)
	if record.ID == "" {
		return fmt.Errorf("run id cannot be empty")
	}
	return writeJSONAtomic(s.runResultPath(record.ID), record)
}

func (s *Service) writeRunArtifacts(id string, artifacts []research.ResearchArtifact) error {
	id = sanitizeID(id)
	if id == "" {
		return fmt.Errorf("run id cannot be empty")
	}
	return writeJSONAtomic(s.runArtifactsPath(id), struct {
		Artifacts []research.ResearchArtifact `json:"artifacts"`
	}{
		Artifacts: artifacts,
	})
}

func (s *Service) readRunRecord(path string) (RunRecord, error) {
	var record RunRecord
	if err := readJSONFile(path, &record); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func (s *Service) readRunArtifacts(path string) ([]research.ResearchArtifact, error) {
	var payload struct {
		Artifacts []research.ResearchArtifact `json:"artifacts"`
	}
	if err := readJSONFile(path, &payload); err != nil {
		return nil, err
	}
	return payload.Artifacts, nil
}

func readJSONFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return fmt.Errorf("empty json file: %s", path)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode json file %q: %w", path, err)
	}
	return nil
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	file, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func sanitizeID(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return ""
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
