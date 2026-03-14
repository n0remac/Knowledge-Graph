package telemetry

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Manager struct {
	cfg                 Config
	eventsCh            chan Event
	writer              *Writer
	discord             *DiscordReporter
	wg                  sync.WaitGroup
	mu                  sync.Mutex
	nextSequenceByTrace map[string]int64
	droppedByTrace      map[string]int64
	closed              bool
}

func NewManager(cfg Config, session *discordgo.Session) (*Manager, error) {
	cfg = cfg.normalized()
	manager := &Manager{
		cfg:                 cfg,
		nextSequenceByTrace: make(map[string]int64),
		droppedByTrace:      make(map[string]int64),
	}
	if !cfg.Enabled {
		return manager, nil
	}

	manager.eventsCh = make(chan Event, cfg.BufferSize)
	if cfg.EnableDiscordReporting && session != nil && strings.TrimSpace(cfg.DiscordDebugChannelID) != "" {
		manager.discord = newDiscordReporter(cfg, newSessionDiscordSender(session), manager.Emit)
	}
	var reportCh chan WrittenEvent
	if manager.discord != nil {
		reportCh = manager.discord.reportCh
	}
	manager.writer = newWriter(cfg, reportCh, manager.droppedCount)

	manager.wg.Add(1)
	go func() {
		defer manager.wg.Done()
		manager.writer.run(manager.eventsCh)
	}()
	if manager.discord != nil {
		manager.wg.Add(1)
		go func() {
			defer manager.wg.Done()
			manager.discord.run()
		}()
	}

	return manager, nil
}

func (m *Manager) Emit(ctx context.Context, stage, kind, summary string, payload map[string]any) {
	if m == nil || !m.cfg.Enabled || m.eventsCh == nil {
		return
	}
	if !m.shouldEmit(stage, kind) {
		return
	}

	traceID := TraceIDFromContext(ctx)
	if strings.TrimSpace(traceID) == "" {
		return
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.nextSequenceByTrace[traceID]++
	sequence := m.nextSequenceByTrace[traceID]
	m.mu.Unlock()

	event := Event{
		TraceID:   traceID,
		Sequence:  sequence,
		Timestamp: time.Now().UTC(),
		Stage:     stage,
		Kind:      kind,
		Summary:   strings.TrimSpace(summary),
		Payload:   payload,
	}

	select {
	case m.eventsCh <- event:
	default:
		m.mu.Lock()
		m.droppedByTrace[traceID]++
		dropped := m.droppedByTrace[traceID]
		m.mu.Unlock()
		log.Printf("telemetry event dropped trace_id=%q kind=%q dropped=%d", traceID, kind, dropped)
	}
}

func (m *Manager) Close() error {
	if m == nil || !m.cfg.Enabled || m.eventsCh == nil {
		return nil
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.eventsCh)
	m.mu.Unlock()

	m.wg.Wait()
	return nil
}

func (m *Manager) droppedCount(traceID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.droppedByTrace[traceID]
}

func (m *Manager) shouldEmit(stage, kind string) bool {
	switch {
	case kind == "ollama_request":
		return m.cfg.WriteRawPromptFiles
	case kind == "ollama_response":
		return m.cfg.WriteRawResponseFiles
	}

	switch stage {
	case StageRuntime:
		return m.cfg.WriteRuntimeEvents
	case StageStore:
		return m.cfg.WriteStoreEvents
	case StageRetrieval:
		return m.cfg.WriteRetrievalEvents
	default:
		return true
	}
}
