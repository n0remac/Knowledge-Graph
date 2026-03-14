package telemetry

import (
	"log"
	"time"
)

type Writer struct {
	cfg             Config
	reportCh        chan<- WrittenEvent
	traceStates     map[string]*traceState
	droppedCountFor func(string) int64
}

type traceState struct {
	index   TraceIndex
	summary TraceSummary
}

func newWriter(cfg Config, reportCh chan<- WrittenEvent, droppedCountFor func(string) int64) *Writer {
	return &Writer{
		cfg:             cfg,
		reportCh:        reportCh,
		traceStates:     make(map[string]*traceState),
		droppedCountFor: droppedCountFor,
	}
}

func (w *Writer) run(events <-chan Event) {
	for event := range events {
		written, err := w.writeEvent(event)
		if err != nil {
			log.Printf("telemetry write failed trace_id=%q kind=%q err=%q", event.TraceID, event.Kind, err.Error())
			continue
		}
		if w.reportCh == nil {
			continue
		}
		select {
		case w.reportCh <- written:
		default:
			log.Printf("telemetry reporter queue full trace_id=%q kind=%q", event.TraceID, event.Kind)
		}
	}
	if w.reportCh != nil {
		close(w.reportCh)
	}
}

func (w *Writer) writeEvent(event Event) (WrittenEvent, error) {
	state := w.traceStates[event.TraceID]
	if state == nil {
		state = &traceState{
			index: TraceIndex{
				TraceID: event.TraceID,
				Status:  "in_progress",
			},
			summary: newTraceSummary(event.TraceID),
		}
		w.traceStates[event.TraceID] = state
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = state.index.UpdatedAt
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = state.index.StartedAt
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = state.summary.UpdatedAt
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	if state.index.StartedAt.IsZero() {
		state.index.StartedAt = event.Timestamp
	}
	state.index.UpdatedAt = event.Timestamp
	state.index.EventCount++
	state.index.LatestStage = event.Stage
	state.index.LatestKind = event.Kind
	state.index.DroppedEventCount = w.droppedCount(event.TraceID)
	if event.Kind == "message_received" {
		state.index.SourceMessage = cloneMap(payloadMap(event.Payload, "message"))
	}

	applyEventToSummary(&state.summary, event)
	state.index.Status = state.summary.Status

	eventPath := eventFilePath(w.cfg.BaseDir, event)
	if err := writeJSONAtomic(eventPath, event); err != nil {
		return WrittenEvent{}, err
	}

	tracePath := traceIndexPath(w.cfg.BaseDir, event.TraceID)
	if err := writeJSONAtomic(tracePath, state.index); err != nil {
		return WrittenEvent{}, err
	}

	summaryPath := traceSummaryPath(w.cfg.BaseDir, event.TraceID)
	if err := writeJSONAtomic(summaryPath, state.summary); err != nil {
		return WrittenEvent{}, err
	}

	return WrittenEvent{
		Event:       event,
		FilePath:    eventPath,
		TraceDir:    traceDirectory(w.cfg.BaseDir, event.TraceID),
		TracePath:   tracePath,
		SummaryPath: summaryPath,
		TraceIndex:  state.index,
		Summary:     state.summary,
	}, nil
}

func (w *Writer) droppedCount(traceID string) int64 {
	if w.droppedCountFor == nil {
		return 0
	}
	return w.droppedCountFor(traceID)
}
