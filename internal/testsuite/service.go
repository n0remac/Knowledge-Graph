package testsuite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/generate"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

var ErrNotFound = errors.New("test suite item not found")

type ServiceConfig struct {
	BaseDir             string
	OllamaBaseURL       string
	OllamaCommand       []string
	RequestTimeout      time.Duration
	DefaultChatModel    string
	DefaultExtractModel string
	DefaultPersona      string
	Telemetry           telemetry.Config
}

type Service struct {
	cfg ServiceConfig
}

func NewService(cfg ServiceConfig) (*Service, error) {
	baseDir := filepath.Clean(strings.TrimSpace(cfg.BaseDir))
	if baseDir == "" || baseDir == "." {
		return nil, fmt.Errorf("test suite base dir cannot be empty")
	}
	if strings.TrimSpace(cfg.OllamaBaseURL) == "" {
		return nil, fmt.Errorf("test suite ollama base url cannot be empty")
	}
	if cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("test suite request timeout must be > 0")
	}
	cfg.BaseDir = baseDir
	cfg.OllamaBaseURL = strings.TrimRight(strings.TrimSpace(cfg.OllamaBaseURL), "/")
	if len(cfg.OllamaCommand) == 0 {
		cfg.OllamaCommand = []string{"ollama"}
	}
	cfg.DefaultChatModel = strings.TrimSpace(cfg.DefaultChatModel)
	cfg.DefaultExtractModel = strings.TrimSpace(cfg.DefaultExtractModel)
	cfg.DefaultPersona = strings.TrimSpace(cfg.DefaultPersona)

	for _, path := range []string{cfg.BaseDir, filepath.Join(cfg.BaseDir, "transcripts"), filepath.Join(cfg.BaseDir, "runs")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, fmt.Errorf("create test suite directory %q: %w", path, err)
		}
	}

	return &Service{cfg: cfg}, nil
}

func (s *Service) Defaults() RunDefaults {
	if s == nil {
		return RunDefaults{}
	}
	return RunDefaults{
		ChatModel:    s.cfg.DefaultChatModel,
		ExtractModel: s.cfg.DefaultExtractModel,
		Persona:      s.cfg.DefaultPersona,
	}
}

func (s *Service) ListModels(ctx context.Context) ([]ModelOption, error) {
	if s == nil {
		return nil, fmt.Errorf("test suite service is not initialized")
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

func (s *Service) ListTranscripts() ([]Transcript, error) {
	if s == nil {
		return nil, fmt.Errorf("test suite service is not initialized")
	}

	entries, err := os.ReadDir(s.transcriptsDir())
	if err != nil {
		return nil, fmt.Errorf("read transcripts dir: %w", err)
	}

	out := make([]Transcript, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		transcript, err := s.readTranscriptFile(filepath.Join(s.transcriptsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, transcript)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAtUnixMs == out[j].UpdatedAtUnixMs {
			return out[i].Name < out[j].Name
		}
		return out[i].UpdatedAtUnixMs > out[j].UpdatedAtUnixMs
	})
	return out, nil
}

func (s *Service) GetTranscript(id string) (Transcript, error) {
	if s == nil {
		return Transcript{}, fmt.Errorf("test suite service is not initialized")
	}

	id = sanitizeID(id)
	if id == "" {
		return Transcript{}, ErrNotFound
	}
	path := s.transcriptPath(id)
	transcript, err := s.readTranscriptFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Transcript{}, ErrNotFound
		}
		return Transcript{}, err
	}
	return transcript, nil
}

func (s *Service) SaveTranscript(input Transcript) (Transcript, error) {
	if s == nil {
		return Transcript{}, fmt.Errorf("test suite service is not initialized")
	}

	nowUnixMs := time.Now().UTC().UnixMilli()
	transcript, err := sanitizeTranscript(input)
	if err != nil {
		return Transcript{}, err
	}

	if transcript.ID == "" {
		transcript.ID, err = s.nextTranscriptID(transcript.Name)
		if err != nil {
			return Transcript{}, err
		}
		transcript.CreatedAtUnixMs = nowUnixMs
	} else {
		existing, getErr := s.GetTranscript(transcript.ID)
		if getErr != nil {
			if errors.Is(getErr, ErrNotFound) {
				return Transcript{}, ErrNotFound
			}
			return Transcript{}, getErr
		}
		transcript.CreatedAtUnixMs = existing.CreatedAtUnixMs
	}
	transcript.UpdatedAtUnixMs = nowUnixMs

	if err := writeJSONAtomic(s.transcriptPath(transcript.ID), transcript); err != nil {
		return Transcript{}, fmt.Errorf("write transcript: %w", err)
	}
	return transcript, nil
}

func (s *Service) DeleteTranscript(id string) error {
	if s == nil {
		return fmt.Errorf("test suite service is not initialized")
	}

	id = sanitizeID(id)
	if id == "" {
		return ErrNotFound
	}
	if err := os.Remove(s.transcriptPath(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("delete transcript: %w", err)
	}
	return nil
}

func (s *Service) RunTranscript(ctx context.Context, request RunRequest) (RunRecord, error) {
	if s == nil {
		return RunRecord{}, fmt.Errorf("test suite service is not initialized")
	}

	req, transcript, err := s.prepareRunRequest(request)
	if err != nil {
		return RunRecord{}, err
	}

	runID, err := s.nextRunID()
	if err != nil {
		return RunRecord{}, err
	}

	runDir := s.runDir(runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return RunRecord{}, fmt.Errorf("create run dir: %w", err)
	}

	record := RunRecord{
		ID:              runID,
		TranscriptID:    transcript.ID,
		TranscriptName:  transcript.Name,
		ChatModel:       req.ChatModel,
		ExtractModel:    req.ExtractModel,
		Persona:         req.Persona,
		Status:          "running",
		StartedAtUnixMs: time.Now().UTC().UnixMilli(),
		ConversationID:  runID,
		StorePath:       filepath.Join(runDir, "conversation-state.json"),
		TelemetryDir:    filepath.Join(runDir, "telemetry"),
		Steps:           make([]RunStepResult, 0, len(transcript.Steps)),
	}
	if err := s.writeRunRecord(record); err != nil {
		return RunRecord{}, err
	}

	finalRecord, err := s.executeRun(ctx, transcript, record)
	if err != nil {
		return RunRecord{}, err
	}
	return finalRecord, nil
}

func (s *Service) GetRun(id string) (RunRecord, error) {
	if s == nil {
		return RunRecord{}, fmt.Errorf("test suite service is not initialized")
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
		return nil, fmt.Errorf("test suite service is not initialized")
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

func (s *Service) executeRun(ctx context.Context, transcript Transcript, record RunRecord) (RunRecord, error) {
	store, telemetryManager, closeAll, err := s.newRunDependencies(record)
	if err != nil {
		record.Status = "failed"
		record.Error = err.Error()
		record.CompletedAtUnixMs = time.Now().UTC().UnixMilli()
		if writeErr := s.writeRunRecord(record); writeErr != nil {
			return RunRecord{}, writeErr
		}
		return record, nil
	}
	defer closeAll()

	client := ollama.NewClient(s.cfg.OllamaBaseURL, s.cfg.RequestTimeout)
	engine := conversation.NewEngine(store, client, record.ExtractModel, s.cfg.RequestTimeout, telemetryManager)
	generator := generate.NewGenerator(client, record.ChatModel, record.Persona, telemetryManager)

	baseTimestamp := record.StartedAtUnixMs
	for i, step := range transcript.Steps {
		userMessage := models.RawMessage{
			MessageID:       fmt.Sprintf("u-%03d", i+1),
			ConversationID:  record.ConversationID,
			AuthorID:        "test-user",
			AuthorRole:      "user",
			Content:         step.Message,
			TimestampUnixMs: baseTimestamp + int64(i*2),
		}

		stepCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
		stepCtx = telemetry.WithTraceID(stepCtx, userMessage.MessageID)
		userResult, stepErr := engine.ProcessMessage(stepCtx, userMessage, conversation.ProcessOptions{})
		cancel()
		if stepErr != nil {
			record = s.failRun(record, fmt.Errorf("process user step %d: %w", i+1, stepErr))
			if writeErr := s.writeRunRecord(record); writeErr != nil {
				return RunRecord{}, writeErr
			}
			return record, nil
		}

		replyCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
		replyCtx = telemetry.WithTraceID(replyCtx, userMessage.MessageID)
		reply, genErr := generator.GenerateReplyFromBrief(replyCtx, userResult.Message, userResult.ResponseContext.Brief)
		cancel()
		if genErr != nil {
			record = s.failRun(record, fmt.Errorf("generate reply for step %d: %w", i+1, genErr))
			if writeErr := s.writeRunRecord(record); writeErr != nil {
				return RunRecord{}, writeErr
			}
			return record, nil
		}
		reply = strings.TrimSpace(reply)

		traceIDs := []string{userMessage.MessageID}
		if reply != "" {
			assistantMessage := models.RawMessage{
				MessageID:        fmt.Sprintf("a-%03d", i+1),
				ConversationID:   record.ConversationID,
				AuthorID:         "assistant",
				AuthorRole:       "assistant",
				Content:          reply,
				TimestampUnixMs:  baseTimestamp + int64(i*2) + 1,
				ReplyToMessageID: userMessage.MessageID,
			}

			assistantCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
			assistantCtx = telemetry.WithTraceID(assistantCtx, assistantMessage.MessageID)
			_, stepErr = engine.ProcessMessage(assistantCtx, assistantMessage, conversation.ProcessOptions{ReplyTarget: &userResult.Message})
			cancel()
			if stepErr != nil {
				record = s.failRun(record, fmt.Errorf("process assistant step %d: %w", i+1, stepErr))
				if writeErr := s.writeRunRecord(record); writeErr != nil {
					return RunRecord{}, writeErr
				}
				return record, nil
			}
			traceIDs = append(traceIDs, assistantMessage.MessageID)
		}

		record.Steps = append(record.Steps, RunStepResult{
			Index:               i + 1,
			UserMessage:         userMessage.Content,
			AssistantReply:      reply,
			SummaryUpdateStatus: userResult.SummaryUpdateStatus,
			WorkingStateVersion: userResult.WorkingState.StateVersion,
			RollingSummary:      userResult.WorkingState.RollingSummary,
			ResponseBrief:       userResult.ResponseContext.Brief,
			TraceIDs:            traceIDs,
		})
		if err := s.writeRunRecord(record); err != nil {
			return RunRecord{}, err
		}
	}

	if err := store.Flush(); err != nil {
		record = s.failRun(record, fmt.Errorf("flush run store: %w", err))
		if writeErr := s.writeRunRecord(record); writeErr != nil {
			return RunRecord{}, writeErr
		}
		return record, nil
	}

	record.Status = "completed"
	record.CompletedAtUnixMs = time.Now().UTC().UnixMilli()
	record.Error = ""
	if err := s.writeRunRecord(record); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func (s *Service) failRun(record RunRecord, err error) RunRecord {
	record.Status = "failed"
	record.CompletedAtUnixMs = time.Now().UTC().UnixMilli()
	if err != nil {
		record.Error = err.Error()
	}
	return record
}

func (s *Service) newRunDependencies(record RunRecord) (*conversation.Store, *telemetry.Manager, func(), error) {
	runTelemetry := s.cfg.Telemetry
	runTelemetry.Enabled = true
	runTelemetry.BaseDir = record.TelemetryDir
	runTelemetry.EnableDiscordReporting = false

	manager, err := telemetry.NewManager(runTelemetry, nil)
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("create telemetry manager: %w", err)
	}

	store, err := conversation.NewStore(record.StorePath, manager)
	if err != nil {
		_ = manager.Close()
		return nil, nil, func() {}, fmt.Errorf("create conversation store: %w", err)
	}

	closeAll := func() {
		_ = store.Close()
		_ = manager.Close()
	}
	return store, manager, closeAll, nil
}

func (s *Service) prepareRunRequest(request RunRequest) (RunRequest, Transcript, error) {
	transcriptID := sanitizeID(request.TranscriptID)
	if transcriptID == "" {
		return RunRequest{}, Transcript{}, fmt.Errorf("transcript_id is required")
	}
	transcript, err := s.GetTranscript(transcriptID)
	if err != nil {
		return RunRequest{}, Transcript{}, err
	}

	req := RunRequest{
		TranscriptID: transcriptID,
		ChatModel:    strings.TrimSpace(request.ChatModel),
		ExtractModel: strings.TrimSpace(request.ExtractModel),
		Persona:      strings.TrimSpace(request.Persona),
	}
	if req.ChatModel == "" {
		req.ChatModel = s.cfg.DefaultChatModel
	}
	if req.ExtractModel == "" {
		req.ExtractModel = s.cfg.DefaultExtractModel
	}
	if req.Persona == "" {
		req.Persona = s.cfg.DefaultPersona
	}
	if req.ChatModel == "" || req.ExtractModel == "" || req.Persona == "" {
		return RunRequest{}, Transcript{}, fmt.Errorf("chat model, extract model, and persona are required")
	}
	return req, transcript, nil
}

func sanitizeTranscript(input Transcript) (Transcript, error) {
	transcript := Transcript{
		ID:          sanitizeID(input.ID),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Steps:       make([]TranscriptStep, 0, len(input.Steps)),
	}
	if transcript.Name == "" {
		return Transcript{}, fmt.Errorf("transcript name cannot be empty")
	}

	for _, step := range input.Steps {
		message := strings.TrimSpace(step.Message)
		if message == "" {
			return Transcript{}, fmt.Errorf("transcript steps cannot contain empty messages")
		}
		transcript.Steps = append(transcript.Steps, TranscriptStep{Message: message})
	}
	if len(transcript.Steps) == 0 {
		return Transcript{}, fmt.Errorf("transcript must contain at least one step")
	}
	return transcript, nil
}

func (s *Service) nextTranscriptID(name string) (string, error) {
	base := sanitizeID(name)
	if base == "" {
		base = "transcript"
	}
	for i := 0; i < 1000; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}
		if _, err := os.Stat(s.transcriptPath(candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("check transcript id: %w", err)
		}
	}
	return "", fmt.Errorf("unable to allocate transcript id")
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

func (s *Service) transcriptsDir() string {
	return filepath.Join(s.cfg.BaseDir, "transcripts")
}

func (s *Service) runsDir() string {
	return filepath.Join(s.cfg.BaseDir, "runs")
}

func (s *Service) transcriptPath(id string) string {
	return filepath.Join(s.transcriptsDir(), sanitizeID(id)+".json")
}

func (s *Service) runDir(id string) string {
	return filepath.Join(s.runsDir(), sanitizeID(id))
}

func (s *Service) runResultPath(id string) string {
	return filepath.Join(s.runDir(id), "result.json")
}

func (s *Service) writeRunRecord(record RunRecord) error {
	record.ID = sanitizeID(record.ID)
	if record.ID == "" {
		return fmt.Errorf("run id cannot be empty")
	}
	return writeJSONAtomic(s.runResultPath(record.ID), record)
}

func (s *Service) readTranscriptFile(path string) (Transcript, error) {
	var transcript Transcript
	if err := readJSONFile(path, &transcript); err != nil {
		return Transcript{}, err
	}
	return transcript, nil
}

func (s *Service) readRunRecord(path string) (RunRecord, error) {
	var record RunRecord
	if err := readJSONFile(path, &record); err != nil {
		return RunRecord{}, err
	}
	return record, nil
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
