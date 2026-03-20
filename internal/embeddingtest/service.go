package embeddingtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/adminstream"
	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
)

var ErrNotFound = errors.New("embedding test item not found")

const defaultTopK = 5
const maxTopK = 20

type ServiceConfig struct {
	BaseDir                string
	OllamaBaseURL          string
	OllamaCommand          []string
	RequestTimeout         time.Duration
	DefaultEmbeddingModel  string
	QdrantBaseURL          string
	QdrantAPIKey           string
	QdrantCollectionPrefix string
}

type Service struct {
	cfg      ServiceConfig
	index    *embedding.Index
	notifier *adminstream.Notifier
}

func NewService(cfg ServiceConfig) (*Service, error) {
	baseDir := filepath.Clean(strings.TrimSpace(cfg.BaseDir))
	if baseDir == "" || baseDir == "." {
		return nil, fmt.Errorf("embedding test base dir cannot be empty")
	}
	if strings.TrimSpace(cfg.OllamaBaseURL) == "" {
		return nil, fmt.Errorf("embedding test ollama base url cannot be empty")
	}
	if cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("embedding test request timeout must be > 0")
	}
	cfg.BaseDir = baseDir
	cfg.OllamaBaseURL = strings.TrimRight(strings.TrimSpace(cfg.OllamaBaseURL), "/")
	cfg.DefaultEmbeddingModel = strings.TrimSpace(cfg.DefaultEmbeddingModel)
	cfg.QdrantBaseURL = strings.TrimRight(strings.TrimSpace(cfg.QdrantBaseURL), "/")
	cfg.QdrantAPIKey = strings.TrimSpace(cfg.QdrantAPIKey)
	cfg.QdrantCollectionPrefix = strings.TrimSpace(cfg.QdrantCollectionPrefix)
	if cfg.DefaultEmbeddingModel == "" {
		return nil, fmt.Errorf("default embedding model cannot be empty")
	}
	if len(cfg.OllamaCommand) == 0 {
		cfg.OllamaCommand = []string{"ollama"}
	}

	for _, path := range []string{cfg.BaseDir, filepath.Join(cfg.BaseDir, "message-sets"), filepath.Join(cfg.BaseDir, "runs")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, fmt.Errorf("create embedding test directory %q: %w", path, err)
		}
	}

	index, err := embedding.New(ollama.NewClient(cfg.OllamaBaseURL, cfg.RequestTimeout), embedding.Config{
		QdrantBaseURL:    cfg.QdrantBaseURL,
		QdrantAPIKey:     cfg.QdrantAPIKey,
		CollectionPrefix: cfg.QdrantCollectionPrefix,
		RequestTimeout:   cfg.RequestTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &Service{cfg: cfg, index: index}, nil
}

func (s *Service) SetNotifier(notifier *adminstream.Notifier) {
	if s == nil {
		return
	}
	s.notifier = notifier
}

func (s *Service) Defaults() Defaults {
	if s == nil {
		return Defaults{}
	}
	return Defaults{EmbeddingModel: s.cfg.DefaultEmbeddingModel}
}

func (s *Service) ListModels(ctx context.Context) ([]ModelOption, error) {
	if s == nil {
		return nil, fmt.Errorf("embedding test service is not initialized")
	}

	cmdArgs := append([]string(nil), s.cfg.OllamaCommand...)
	if len(cmdArgs) == 0 || strings.TrimSpace(cmdArgs[0]) == "" {
		return nil, fmt.Errorf("ollama command is not configured")
	}
	cmdArgs = append(cmdArgs, "list")

	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	if _, ok := callCtx.Deadline(); !ok && s.cfg.RequestTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(callCtx, s.cfg.RequestTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(callCtx, cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("ollama list failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("ollama list failed: %w", err)
	}

	models := parseOllamaListOutput(string(output))
	if len(models) == 0 {
		return nil, fmt.Errorf("ollama list returned no models")
	}
	return models, nil
}

func (s *Service) ListMessageSets() ([]MessageSet, error) {
	if s == nil {
		return nil, fmt.Errorf("embedding test service is not initialized")
	}

	entries, err := os.ReadDir(s.messageSetsDir())
	if err != nil {
		return nil, fmt.Errorf("read message sets dir: %w", err)
	}

	out := make([]MessageSet, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		set, err := s.readMessageSetFile(filepath.Join(s.messageSetsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, set)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAtUnixMs == out[j].UpdatedAtUnixMs {
			return out[i].Name < out[j].Name
		}
		return out[i].UpdatedAtUnixMs > out[j].UpdatedAtUnixMs
	})
	return out, nil
}

func (s *Service) GetMessageSet(id string) (MessageSet, error) {
	if s == nil {
		return MessageSet{}, fmt.Errorf("embedding test service is not initialized")
	}

	id = sanitizeID(id)
	if id == "" {
		return MessageSet{}, ErrNotFound
	}
	set, err := s.readMessageSetFile(s.messageSetPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MessageSet{}, ErrNotFound
		}
		return MessageSet{}, err
	}
	return set, nil
}

func (s *Service) SaveMessageSet(input MessageSet) (MessageSet, error) {
	if s == nil {
		return MessageSet{}, fmt.Errorf("embedding test service is not initialized")
	}

	nowUnixMs := time.Now().UTC().UnixMilli()
	set, err := sanitizeMessageSet(input)
	if err != nil {
		return MessageSet{}, err
	}

	if set.ID == "" {
		set.ID, err = s.nextMessageSetID(set.Name)
		if err != nil {
			return MessageSet{}, err
		}
		set.CreatedAtUnixMs = nowUnixMs
	} else {
		existing, getErr := s.GetMessageSet(set.ID)
		if getErr != nil {
			if errors.Is(getErr, ErrNotFound) {
				return MessageSet{}, ErrNotFound
			}
			return MessageSet{}, getErr
		}
		set.CreatedAtUnixMs = existing.CreatedAtUnixMs
	}
	set.UpdatedAtUnixMs = nowUnixMs

	if err := writeJSONAtomic(s.messageSetPath(set.ID), set); err != nil {
		return MessageSet{}, fmt.Errorf("write message set: %w", err)
	}
	s.publishChange()
	return set, nil
}

func (s *Service) DeleteMessageSet(id string) error {
	if s == nil {
		return fmt.Errorf("embedding test service is not initialized")
	}

	id = sanitizeID(id)
	if id == "" {
		return ErrNotFound
	}
	if err := os.Remove(s.messageSetPath(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("delete message set: %w", err)
	}
	s.publishChange()
	return nil
}

func (s *Service) RunQuery(ctx context.Context, request RunRequest) (RunRecord, error) {
	if s == nil {
		return RunRecord{}, fmt.Errorf("embedding test service is not initialized")
	}

	req, set, err := s.prepareRunRequest(request)
	if err != nil {
		return RunRecord{}, err
	}

	runID, err := s.nextRunID()
	if err != nil {
		return RunRecord{}, err
	}

	record := RunRecord{
		ID:              sanitizeID(runID),
		MessageSetID:    set.ID,
		MessageSetName:  set.Name,
		EmbeddingModel:  req.EmbeddingModel,
		Query:           req.Query,
		TopK:            req.TopK,
		Status:          "running",
		StartedAtUnixMs: time.Now().UTC().UnixMilli(),
		CollectionName:  s.index.CollectionName(req.EmbeddingModel),
		Results:         make([]SearchResult, 0, req.TopK),
	}
	if err := s.writeRunRecord(record); err != nil {
		return RunRecord{}, err
	}

	docs := make([]embedding.Document, 0, len(set.Messages))
	for idx, message := range set.Messages {
		docs = append(docs, embedding.Document{
			DocumentID: set.ID + ":" + sanitizeID(strconv.Itoa(idx)),
			Text:       message.Text,
			Payload: map[string]any{
				"message_set_id": set.ID,
				"message_index":  idx,
			},
		})
	}

	if _, err := s.index.UpsertDocuments(ctx, req.EmbeddingModel, docs); err != nil {
		record.Status = "failed"
		record.CompletedAtUnixMs = time.Now().UTC().UnixMilli()
		record.Error = err.Error()
		if writeErr := s.writeRunRecord(record); writeErr != nil {
			return RunRecord{}, writeErr
		}
		s.publishChange()
		return record, nil
	}

	searchResult, err := s.index.Search(ctx, req.EmbeddingModel, req.Query, req.TopK, map[string]string{"message_set_id": set.ID})
	if err != nil {
		record.Status = "failed"
		record.CompletedAtUnixMs = time.Now().UTC().UnixMilli()
		record.Error = err.Error()
		if writeErr := s.writeRunRecord(record); writeErr != nil {
			return RunRecord{}, writeErr
		}
		s.publishChange()
		return record, nil
	}

	record.CollectionName = searchResult.CollectionName
	record.Results = make([]SearchResult, 0, len(searchResult.Results))
	for _, item := range searchResult.Results {
		messageIndex := 0
		switch value := item.Payload["message_index"].(type) {
		case float64:
			messageIndex = int(value)
		case int:
			messageIndex = value
		}
		record.Results = append(record.Results, SearchResult{
			Rank:         item.Rank,
			Score:        item.Score,
			MessageIndex: messageIndex,
			MessageText:  item.Text,
		})
	}
	record.Status = "completed"
	record.CompletedAtUnixMs = time.Now().UTC().UnixMilli()
	record.Error = ""
	if err := s.writeRunRecord(record); err != nil {
		return RunRecord{}, err
	}
	s.publishChange()
	return record, nil
}

func (s *Service) GetRun(id string) (RunRecord, error) {
	if s == nil {
		return RunRecord{}, fmt.Errorf("embedding test service is not initialized")
	}

	id = sanitizeID(id)
	if id == "" {
		return RunRecord{}, ErrNotFound
	}
	record, err := s.readRunRecord(s.runResultPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RunRecord{}, ErrNotFound
		}
		return RunRecord{}, err
	}
	return record, nil
}

func (s *Service) ListRuns(limit int) ([]RunRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("embedding test service is not initialized")
	}
	if limit <= 0 {
		limit = 20
	}

	entries, err := os.ReadDir(s.runsDir())
	if err != nil {
		return nil, fmt.Errorf("read runs dir: %w", err)
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
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAtUnixMs > out[j].StartedAtUnixMs
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Service) prepareRunRequest(request RunRequest) (RunRequest, MessageSet, error) {
	messageSetID := sanitizeID(request.MessageSetID)
	if messageSetID == "" {
		return RunRequest{}, MessageSet{}, fmt.Errorf("message_set_id is required")
	}
	set, err := s.GetMessageSet(messageSetID)
	if err != nil {
		return RunRequest{}, MessageSet{}, err
	}

	req := RunRequest{
		MessageSetID:   messageSetID,
		EmbeddingModel: strings.TrimSpace(request.EmbeddingModel),
		Query:          strings.TrimSpace(request.Query),
		TopK:           request.TopK,
	}
	if req.EmbeddingModel == "" {
		req.EmbeddingModel = s.cfg.DefaultEmbeddingModel
	}
	if req.TopK == 0 {
		req.TopK = defaultTopK
	}
	if req.EmbeddingModel == "" {
		return RunRequest{}, MessageSet{}, fmt.Errorf("embedding model is required")
	}
	if req.Query == "" {
		return RunRequest{}, MessageSet{}, fmt.Errorf("query cannot be empty")
	}
	if req.TopK <= 0 || req.TopK > maxTopK {
		return RunRequest{}, MessageSet{}, fmt.Errorf("top_k must be between 1 and %d", maxTopK)
	}
	return req, set, nil
}

func (s *Service) publishChange() {
	if s == nil || s.notifier == nil {
		return
	}
	s.notifier.Publish(adminstream.VerticalEmbeddings)
}

func sanitizeMessageSet(input MessageSet) (MessageSet, error) {
	set := MessageSet{
		ID:          sanitizeID(input.ID),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Messages:    make([]MessageEntry, 0, len(input.Messages)),
	}
	if set.Name == "" {
		return MessageSet{}, fmt.Errorf("message set name cannot be empty")
	}
	for _, entry := range input.Messages {
		text := strings.TrimSpace(entry.Text)
		if text == "" {
			return MessageSet{}, fmt.Errorf("message sets cannot contain empty messages")
		}
		set.Messages = append(set.Messages, MessageEntry{Text: text})
	}
	if len(set.Messages) == 0 {
		return MessageSet{}, fmt.Errorf("message set must contain at least one message")
	}
	return set, nil
}

func (s *Service) messageSetsDir() string {
	return filepath.Join(s.cfg.BaseDir, "message-sets")
}

func (s *Service) runsDir() string {
	return filepath.Join(s.cfg.BaseDir, "runs")
}

func (s *Service) messageSetPath(id string) string {
	return filepath.Join(s.messageSetsDir(), sanitizeID(id)+".json")
}

func (s *Service) runDir(id string) string {
	return filepath.Join(s.runsDir(), sanitizeID(id))
}

func (s *Service) runResultPath(id string) string {
	return filepath.Join(s.runDir(id), "result.json")
}

func (s *Service) nextMessageSetID(name string) (string, error) {
	base := sanitizeID(name)
	if base == "" {
		base = "message-set"
	}
	for i := 0; i < 1000; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}
		if _, err := os.Stat(s.messageSetPath(candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("check message set id: %w", err)
		}
	}
	return "", fmt.Errorf("unable to allocate message set id")
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
			return "", fmt.Errorf("check run id: %w", err)
		}
	}
	return "", fmt.Errorf("unable to allocate run id")
}

func (s *Service) writeRunRecord(record RunRecord) error {
	record.ID = sanitizeID(record.ID)
	if record.ID == "" {
		return fmt.Errorf("run id cannot be empty")
	}
	return writeJSONAtomic(s.runResultPath(record.ID), record)
}

func (s *Service) readMessageSetFile(path string) (MessageSet, error) {
	var set MessageSet
	if err := readJSONFile(path, &set); err != nil {
		return MessageSet{}, err
	}
	return set, nil
}

func (s *Service) readRunRecord(path string) (RunRecord, error) {
	var record RunRecord
	if err := readJSONFile(path, &record); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func parseOllamaListOutput(raw string) []ModelOption {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	out := make([]ModelOption, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name == "" || strings.EqualFold(name, "name") {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, ModelOption{Name: name})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
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
