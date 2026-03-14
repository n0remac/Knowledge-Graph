package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func traceDirectory(baseDir, traceID string) string {
	traceID = sanitizeFileComponent(traceID)
	if traceID == "" {
		traceID = "unknown-trace"
	}
	return filepath.Join(baseDir, traceID)
}

func eventFileName(sequence int64, stage, kind string) string {
	return fmt.Sprintf("%03d_%s_%s.json", sequence, sanitizeFileComponent(stage), sanitizeFileComponent(kind))
}

func traceIndexPath(baseDir, traceID string) string {
	return filepath.Join(traceDirectory(baseDir, traceID), "trace.json")
}

func traceSummaryPath(baseDir, traceID string) string {
	return filepath.Join(traceDirectory(baseDir, traceID), "summary.json")
}

func eventFilePath(baseDir string, event Event) string {
	return filepath.Join(traceDirectory(baseDir, event.TraceID), eventFileName(event.Sequence, event.Stage, event.Kind))
}

func sanitizeFileComponent(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	var builder strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "_.")
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
