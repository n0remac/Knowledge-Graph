package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/adminstream"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
	_ "modernc.org/sqlite"
)

type Store struct {
	path      string
	db        *sql.DB
	telemetry *telemetry.Manager
	notifier  *adminstream.Notifier
}

type ClaimExtractionRecord struct {
	MessageID         string
	ConversationID    string
	Claims            []models.Claim
	Status            string
	Model             string
	Raw               string
	ExtractedAtUnixMs int64
}

type ClaimRecord struct {
	ClaimID           string
	MessageID         string
	ConversationID    string
	Subject           string
	Predicate         string
	Object            string
	SourceMessageID   string
	ExtractorModel    string
	ExtractedAtUnixMs int64
}

type VectorDocumentRecord struct {
	DocumentID      string
	Kind            string
	MessageID       string
	ClaimID         string
	ConversationID  string
	Content         string
	EmbeddingModel  string
	IndexStatus     string
	IndexedAtUnixMs int64
	CollectionName  string
	LastError       string
	Payload         map[string]any
}

func NewStore(path string, manager *telemetry.Manager) (*Store, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("memory store path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create memory store directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	store := &Store{
		path:      path,
		db:        db,
		telemetry: manager,
	}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) SetNotifier(notifier *adminstream.Notifier) {
	if s == nil {
		return
	}
	s.notifier = notifier
}

func (s *Store) SaveMessage(ctx context.Context, input models.RawMessage) (models.RawMessage, error) {
	message := sanitizeRawMessage(input)
	if message.MessageID == "" || message.ConversationID == "" || message.AuthorID == "" || message.AuthorRole == "" {
		err := fmt.Errorf("invalid raw message after sanitization")
		s.emitError(ctx, "memory_save_message_error", "memory store rejected raw message", err, map[string]any{"input": input})
		return models.RawMessage{}, err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO messages (
			message_id, conversation_id, sequence_number, author_id, author_role, content, timestamp_unix_ms, reply_to_message_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			conversation_id = excluded.conversation_id,
			sequence_number = excluded.sequence_number,
			author_id = excluded.author_id,
			author_role = excluded.author_role,
			content = excluded.content,
			timestamp_unix_ms = excluded.timestamp_unix_ms,
			reply_to_message_id = excluded.reply_to_message_id
	`, message.MessageID, message.ConversationID, message.SequenceNumber, message.AuthorID, message.AuthorRole, message.Content, message.TimestampUnixMs, nullableString(message.ReplyToMessageID))
	if err != nil {
		s.emitError(ctx, "memory_save_message_error", "memory store failed to save raw message", err, map[string]any{"message_id": message.MessageID})
		return models.RawMessage{}, fmt.Errorf("save memory message: %w", err)
	}

	s.emit(ctx, "memory_save_message", "memory store saved raw message", map[string]any{
		"message_id":      message.MessageID,
		"conversation_id": message.ConversationID,
		"author_id":       message.AuthorID,
	})
	s.publishChange()
	return message, nil
}

func (s *Store) SaveClaimsExtraction(ctx context.Context, record ClaimExtractionRecord) ([]ClaimRecord, error) {
	record = sanitizeClaimExtractionRecord(record)
	if record.MessageID == "" || record.ConversationID == "" {
		err := fmt.Errorf("message extraction identifiers cannot be empty")
		s.emitError(ctx, "memory_save_extraction_error", "memory store rejected extraction", err, map[string]any{"message_id": record.MessageID})
		return nil, err
	}

	claimsJSON, err := json.Marshal(record.Claims)
	if err != nil {
		return nil, fmt.Errorf("marshal claims json: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin extraction transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO message_extractions (
			message_id, conversation_id, claims_json, claims_status, claims_model_version, raw_claims_output, extracted_at_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			conversation_id = excluded.conversation_id,
			claims_json = excluded.claims_json,
			claims_status = excluded.claims_status,
			claims_model_version = excluded.claims_model_version,
			raw_claims_output = excluded.raw_claims_output,
			extracted_at_unix_ms = excluded.extracted_at_unix_ms
	`, record.MessageID, record.ConversationID, string(claimsJSON), record.Status, record.Model, record.Raw, record.ExtractedAtUnixMs); err != nil {
		return nil, fmt.Errorf("upsert message extraction: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM claims WHERE message_id = ?`, record.MessageID); err != nil {
		return nil, fmt.Errorf("delete existing claims: %w", err)
	}

	claims := make([]ClaimRecord, 0, len(record.Claims))
	for _, claim := range record.Claims {
		stored := ClaimRecord{
			ClaimID:           claimID(record.MessageID, claim.Subject, claim.Predicate, claim.Object),
			MessageID:         record.MessageID,
			ConversationID:    record.ConversationID,
			Subject:           strings.TrimSpace(claim.Subject),
			Predicate:         strings.TrimSpace(claim.Predicate),
			Object:            strings.TrimSpace(claim.Object),
			SourceMessageID:   strings.TrimSpace(claim.SourceMessageID),
			ExtractorModel:    record.Model,
			ExtractedAtUnixMs: record.ExtractedAtUnixMs,
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO claims (
				claim_id, message_id, conversation_id, subject, predicate, object, source_message_id, extractor_model, extracted_at_unix_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(claim_id) DO UPDATE SET
				message_id = excluded.message_id,
				conversation_id = excluded.conversation_id,
				subject = excluded.subject,
				predicate = excluded.predicate,
				object = excluded.object,
				source_message_id = excluded.source_message_id,
				extractor_model = excluded.extractor_model,
				extracted_at_unix_ms = excluded.extracted_at_unix_ms
		`, stored.ClaimID, stored.MessageID, stored.ConversationID, stored.Subject, stored.Predicate, stored.Object, stored.SourceMessageID, stored.ExtractorModel, stored.ExtractedAtUnixMs); err != nil {
			return nil, fmt.Errorf("insert claim %q: %w", stored.ClaimID, err)
		}
		claims = append(claims, stored)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit extraction transaction: %w", err)
	}

	s.emit(ctx, "memory_save_extraction", "memory store saved claim extraction", map[string]any{
		"message_id":   record.MessageID,
		"status":       record.Status,
		"claim_count":  len(claims),
		"model":        record.Model,
		"conversation": record.ConversationID,
	})
	s.publishChange()
	return claims, nil
}

func (s *Store) UpsertVectorDocuments(ctx context.Context, docs []VectorDocumentRecord) error {
	if len(docs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vector document transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, doc := range docs {
		doc = sanitizeVectorDocumentRecord(doc)
		if doc.DocumentID == "" || doc.Kind == "" || doc.ConversationID == "" || doc.Content == "" || doc.EmbeddingModel == "" {
			return fmt.Errorf("vector document is missing required fields")
		}
		payloadJSON, marshalErr := json.Marshal(doc.Payload)
		if marshalErr != nil {
			return fmt.Errorf("marshal vector payload for %q: %w", doc.DocumentID, marshalErr)
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO vector_documents (
				document_id, kind, message_id, claim_id, conversation_id, content, embedding_model,
				index_status, indexed_at_unix_ms, collection_name, last_error, payload_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(document_id) DO UPDATE SET
				kind = excluded.kind,
				message_id = excluded.message_id,
				claim_id = excluded.claim_id,
				conversation_id = excluded.conversation_id,
				content = excluded.content,
				embedding_model = excluded.embedding_model,
				index_status = excluded.index_status,
				indexed_at_unix_ms = excluded.indexed_at_unix_ms,
				collection_name = excluded.collection_name,
				last_error = excluded.last_error,
				payload_json = excluded.payload_json
		`, doc.DocumentID, doc.Kind, nullableString(doc.MessageID), nullableString(doc.ClaimID), doc.ConversationID, doc.Content, doc.EmbeddingModel, doc.IndexStatus, nullableInt64(doc.IndexedAtUnixMs), nullableString(doc.CollectionName), doc.LastError, string(payloadJSON)); err != nil {
			return fmt.Errorf("upsert vector document %q: %w", doc.DocumentID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit vector document transaction: %w", err)
	}
	s.publishChange()
	return nil
}

func (s *Store) MarkVectorDocumentsIndexed(ctx context.Context, documentIDs []string, collectionName string, indexedAtUnixMs int64) error {
	return s.updateVectorDocumentStatus(ctx, documentIDs, "indexed", collectionName, indexedAtUnixMs, "")
}

func (s *Store) MarkVectorDocumentsFailed(ctx context.Context, documentIDs []string, indexErr error) error {
	lastError := ""
	if indexErr != nil {
		lastError = strings.TrimSpace(indexErr.Error())
	}
	return s.updateVectorDocumentStatus(ctx, documentIDs, "failed", "", 0, lastError)
}

func (s *Store) GetMessageByID(ctx context.Context, messageID string) (models.RawMessage, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT message_id, conversation_id, sequence_number, author_id, author_role, content, timestamp_unix_ms, COALESCE(reply_to_message_id, '')
		FROM messages
		WHERE message_id = ?
	`, strings.TrimSpace(messageID))

	var message models.RawMessage
	if err := row.Scan(&message.MessageID, &message.ConversationID, &message.SequenceNumber, &message.AuthorID, &message.AuthorRole, &message.Content, &message.TimestampUnixMs, &message.ReplyToMessageID); err != nil {
		if err == sql.ErrNoRows {
			return models.RawMessage{}, false, nil
		}
		return models.RawMessage{}, false, fmt.Errorf("get memory message: %w", err)
	}
	return message, true, nil
}

func (s *Store) GetMessageExtractionByID(ctx context.Context, messageID string) (ClaimExtractionRecord, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT message_id, conversation_id, claims_json, claims_status, claims_model_version, raw_claims_output, extracted_at_unix_ms
		FROM message_extractions
		WHERE message_id = ?
	`, strings.TrimSpace(messageID))

	var record ClaimExtractionRecord
	var claimsJSON string
	if err := row.Scan(&record.MessageID, &record.ConversationID, &claimsJSON, &record.Status, &record.Model, &record.Raw, &record.ExtractedAtUnixMs); err != nil {
		if err == sql.ErrNoRows {
			return ClaimExtractionRecord{}, false, nil
		}
		return ClaimExtractionRecord{}, false, fmt.Errorf("get claim extraction: %w", err)
	}
	if err := json.Unmarshal([]byte(claimsJSON), &record.Claims); err != nil {
		return ClaimExtractionRecord{}, false, fmt.Errorf("decode claim extraction json: %w", err)
	}
	return record, true, nil
}

func (s *Store) GetClaimsByMessageID(ctx context.Context, messageID string) ([]ClaimRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT claim_id, message_id, conversation_id, subject, predicate, object, source_message_id, extractor_model, extracted_at_unix_ms
		FROM claims
		WHERE message_id = ?
	`, strings.TrimSpace(messageID))
	if err != nil {
		return nil, fmt.Errorf("query claims by message: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]ClaimRecord, 0)
	for rows.Next() {
		var claim ClaimRecord
		if err := rows.Scan(&claim.ClaimID, &claim.MessageID, &claim.ConversationID, &claim.Subject, &claim.Predicate, &claim.Object, &claim.SourceMessageID, &claim.ExtractorModel, &claim.ExtractedAtUnixMs); err != nil {
			return nil, fmt.Errorf("scan claim: %w", err)
		}
		out = append(out, claim)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ClaimID < out[j].ClaimID
	})
	return out, rows.Err()
}

func (s *Store) GetVectorDocumentsByMessageID(ctx context.Context, messageID string) ([]VectorDocumentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT document_id, kind, COALESCE(message_id, ''), COALESCE(claim_id, ''), conversation_id, content, embedding_model,
		       index_status, COALESCE(indexed_at_unix_ms, 0), COALESCE(collection_name, ''), COALESCE(last_error, ''), payload_json
		FROM vector_documents
		WHERE message_id = ?
	`, strings.TrimSpace(messageID))
	if err != nil {
		return nil, fmt.Errorf("query vector documents by message: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]VectorDocumentRecord, 0)
	for rows.Next() {
		var doc VectorDocumentRecord
		var payloadJSON string
		if err := rows.Scan(&doc.DocumentID, &doc.Kind, &doc.MessageID, &doc.ClaimID, &doc.ConversationID, &doc.Content, &doc.EmbeddingModel, &doc.IndexStatus, &doc.IndexedAtUnixMs, &doc.CollectionName, &doc.LastError, &payloadJSON); err != nil {
			return nil, fmt.Errorf("scan vector document: %w", err)
		}
		if payloadJSON != "" {
			if err := json.Unmarshal([]byte(payloadJSON), &doc.Payload); err != nil {
				return nil, fmt.Errorf("decode vector payload: %w", err)
			}
		}
		out = append(out, doc)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DocumentID < out[j].DocumentID
	})
	return out, rows.Err()
}

func (s *Store) init() error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS messages (
			message_id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			sequence_number INTEGER NOT NULL,
			author_id TEXT NOT NULL,
			author_role TEXT NOT NULL,
			content TEXT NOT NULL,
			timestamp_unix_ms INTEGER NOT NULL,
			reply_to_message_id TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS message_extractions (
			message_id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			claims_json TEXT NOT NULL,
			claims_status TEXT NOT NULL,
			claims_model_version TEXT NOT NULL,
			raw_claims_output TEXT NOT NULL,
			extracted_at_unix_ms INTEGER NOT NULL,
			FOREIGN KEY(message_id) REFERENCES messages(message_id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS claims (
			claim_id TEXT PRIMARY KEY,
			message_id TEXT NOT NULL,
			conversation_id TEXT NOT NULL,
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			source_message_id TEXT NOT NULL,
			extractor_model TEXT NOT NULL,
			extracted_at_unix_ms INTEGER NOT NULL,
			FOREIGN KEY(message_id) REFERENCES messages(message_id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS vector_documents (
			document_id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			message_id TEXT,
			claim_id TEXT,
			conversation_id TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding_model TEXT NOT NULL,
			index_status TEXT NOT NULL,
			indexed_at_unix_ms INTEGER,
			collection_name TEXT,
			last_error TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL
		);`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize memory store schema: %w", err)
		}
	}
	return nil
}

func (s *Store) updateVectorDocumentStatus(ctx context.Context, documentIDs []string, status, collectionName string, indexedAtUnixMs int64, lastError string) error {
	status = normalizeIndexStatus(status)
	updated := false
	for _, documentID := range documentIDs {
		documentID = strings.TrimSpace(documentID)
		if documentID == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE vector_documents
			SET index_status = ?, indexed_at_unix_ms = ?, collection_name = ?, last_error = ?
			WHERE document_id = ?
		`, status, nullableInt64(indexedAtUnixMs), nullableString(collectionName), lastError, documentID); err != nil {
			return fmt.Errorf("update vector document status for %q: %w", documentID, err)
		}
		updated = true
	}
	if updated {
		s.publishChange()
	}
	return nil
}

func sanitizeRawMessage(input models.RawMessage) models.RawMessage {
	role := strings.ToLower(strings.TrimSpace(input.AuthorRole))
	switch role {
	case "user", "assistant":
	default:
		role = ""
	}

	return models.RawMessage{
		MessageID:        strings.TrimSpace(input.MessageID),
		ConversationID:   strings.TrimSpace(input.ConversationID),
		SequenceNumber:   input.SequenceNumber,
		AuthorID:         strings.TrimSpace(input.AuthorID),
		AuthorRole:       role,
		Content:          strings.TrimSpace(input.Content),
		TimestampUnixMs:  input.TimestampUnixMs,
		ReplyToMessageID: strings.TrimSpace(input.ReplyToMessageID),
	}
}

func sanitizeClaimExtractionRecord(input ClaimExtractionRecord) ClaimExtractionRecord {
	output := input
	output.MessageID = strings.TrimSpace(output.MessageID)
	output.ConversationID = strings.TrimSpace(output.ConversationID)
	output.Status = normalizeStatus(output.Status)
	output.Model = strings.TrimSpace(output.Model)
	output.Raw = strings.TrimSpace(output.Raw)
	for idx := range output.Claims {
		output.Claims[idx].Subject = strings.TrimSpace(output.Claims[idx].Subject)
		output.Claims[idx].Predicate = strings.TrimSpace(output.Claims[idx].Predicate)
		output.Claims[idx].Object = strings.TrimSpace(output.Claims[idx].Object)
		output.Claims[idx].SourceMessageID = strings.TrimSpace(output.Claims[idx].SourceMessageID)
	}
	return output
}

func sanitizeVectorDocumentRecord(input VectorDocumentRecord) VectorDocumentRecord {
	output := input
	output.DocumentID = strings.TrimSpace(output.DocumentID)
	output.Kind = strings.TrimSpace(output.Kind)
	output.MessageID = strings.TrimSpace(output.MessageID)
	output.ClaimID = strings.TrimSpace(output.ClaimID)
	output.ConversationID = strings.TrimSpace(output.ConversationID)
	output.Content = strings.TrimSpace(output.Content)
	output.EmbeddingModel = strings.TrimSpace(output.EmbeddingModel)
	output.IndexStatus = normalizeIndexStatus(output.IndexStatus)
	output.CollectionName = strings.TrimSpace(output.CollectionName)
	output.LastError = strings.TrimSpace(output.LastError)
	if output.Payload == nil {
		output.Payload = make(map[string]any)
	}
	return output
}

func normalizeStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ok":
		return "ok"
	case "partial":
		return "partial"
	case "failed":
		return "failed"
	default:
		return "failed"
	}
}

func normalizeIndexStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "indexed":
		return "indexed"
	case "failed":
		return "failed"
	default:
		return "pending"
	}
}

func claimID(messageID, subject, predicate, object string) string {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(strings.TrimSpace(messageID)))
	_, _ = hasher.Write([]byte{':'})
	_, _ = hasher.Write([]byte(strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(subject)), " "))))
	_, _ = hasher.Write([]byte{':'})
	_, _ = hasher.Write([]byte(strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(predicate)), " "))))
	_, _ = hasher.Write([]byte{':'})
	_, _ = hasher.Write([]byte(strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(object)), " "))))
	return "claim-" + strconv.FormatUint(hasher.Sum64(), 16)
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullableInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func (s *Store) emit(ctx context.Context, kind, summary string, payload map[string]any) {
	if s == nil || s.telemetry == nil {
		return
	}
	s.telemetry.Emit(ctx, telemetry.StageMemory, kind, summary, payload)
}

func (s *Store) emitError(ctx context.Context, kind, summary string, err error, payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any, 1)
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	s.emit(ctx, kind, summary, payload)
}

func (s *Store) publishChange() {
	if s == nil || s.notifier == nil {
		return
	}
	s.notifier.Publish(adminstream.VerticalMemory)
}
