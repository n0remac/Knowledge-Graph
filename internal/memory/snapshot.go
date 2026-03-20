package memory

import (
	"context"
	"encoding/json"
	"fmt"
)

type SnapshotOptions struct {
	RecentMessages        int
	RecentClaims          int
	RecentVectorDocuments int
}

type Snapshot struct {
	MessageCount        int64
	ExtractionCount     int64
	ClaimCount          int64
	VectorDocumentCount int64
	RecentMessages      []MessageSummary
	RecentClaims        []ClaimRecord
	RecentVectorDocs    []VectorDocumentRecord
}

type MessageSummary struct {
	MessageID       string
	ConversationID  string
	AuthorID        string
	AuthorRole      string
	Content         string
	TimestampUnixMs int64
}

func (s *Store) Snapshot(ctx context.Context, options SnapshotOptions) (Snapshot, error) {
	if s == nil || s.db == nil {
		return Snapshot{}, fmt.Errorf("memory store is not initialized")
	}
	if options.RecentMessages <= 0 {
		options.RecentMessages = 8
	}
	if options.RecentClaims <= 0 {
		options.RecentClaims = 8
	}
	if options.RecentVectorDocuments <= 0 {
		options.RecentVectorDocuments = 8
	}

	out := Snapshot{}
	var err error
	if out.MessageCount, err = s.count(ctx, "SELECT COUNT(*) FROM messages"); err != nil {
		return Snapshot{}, err
	}
	if out.ExtractionCount, err = s.count(ctx, "SELECT COUNT(*) FROM message_extractions"); err != nil {
		return Snapshot{}, err
	}
	if out.ClaimCount, err = s.count(ctx, "SELECT COUNT(*) FROM claims"); err != nil {
		return Snapshot{}, err
	}
	if out.VectorDocumentCount, err = s.count(ctx, "SELECT COUNT(*) FROM vector_documents"); err != nil {
		return Snapshot{}, err
	}
	if out.RecentMessages, err = s.recentMessages(ctx, options.RecentMessages); err != nil {
		return Snapshot{}, err
	}
	if out.RecentClaims, err = s.recentClaims(ctx, options.RecentClaims); err != nil {
		return Snapshot{}, err
	}
	if out.RecentVectorDocs, err = s.recentVectorDocuments(ctx, options.RecentVectorDocuments); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

func (s *Store) count(ctx context.Context, query string) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count query failed: %w", err)
	}
	return count, nil
}

func (s *Store) recentMessages(ctx context.Context, limit int) ([]MessageSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT message_id, conversation_id, author_id, author_role, content, timestamp_unix_ms
		FROM messages
		ORDER BY timestamp_unix_ms DESC, message_id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent memory messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]MessageSummary, 0, limit)
	for rows.Next() {
		var item MessageSummary
		if err := rows.Scan(&item.MessageID, &item.ConversationID, &item.AuthorID, &item.AuthorRole, &item.Content, &item.TimestampUnixMs); err != nil {
			return nil, fmt.Errorf("scan recent memory message: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) recentClaims(ctx context.Context, limit int) ([]ClaimRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT claim_id, message_id, conversation_id, subject, predicate, object, source_message_id, extractor_model, extracted_at_unix_ms
		FROM claims
		ORDER BY extracted_at_unix_ms DESC, claim_id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent memory claims: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ClaimRecord, 0, limit)
	for rows.Next() {
		var claim ClaimRecord
		if err := rows.Scan(&claim.ClaimID, &claim.MessageID, &claim.ConversationID, &claim.Subject, &claim.Predicate, &claim.Object, &claim.SourceMessageID, &claim.ExtractorModel, &claim.ExtractedAtUnixMs); err != nil {
			return nil, fmt.Errorf("scan recent memory claim: %w", err)
		}
		out = append(out, claim)
	}
	return out, rows.Err()
}

func (s *Store) recentVectorDocuments(ctx context.Context, limit int) ([]VectorDocumentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT document_id, kind, COALESCE(message_id, ''), COALESCE(claim_id, ''), conversation_id, content, embedding_model,
		       index_status, COALESCE(indexed_at_unix_ms, 0), COALESCE(collection_name, ''), COALESCE(last_error, ''), payload_json
		FROM vector_documents
		ORDER BY COALESCE(indexed_at_unix_ms, 0) DESC, document_id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent memory vector documents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]VectorDocumentRecord, 0, limit)
	for rows.Next() {
		var doc VectorDocumentRecord
		var payloadJSON string
		if err := rows.Scan(&doc.DocumentID, &doc.Kind, &doc.MessageID, &doc.ClaimID, &doc.ConversationID, &doc.Content, &doc.EmbeddingModel, &doc.IndexStatus, &doc.IndexedAtUnixMs, &doc.CollectionName, &doc.LastError, &payloadJSON); err != nil {
			return nil, fmt.Errorf("scan recent memory vector document: %w", err)
		}
		if payloadJSON != "" {
			if err := decodePayload(payloadJSON, &doc.Payload); err != nil {
				return nil, err
			}
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func decodePayload(raw string, target *map[string]any) error {
	if raw == "" {
		return nil
	}
	if target == nil {
		return fmt.Errorf("payload target cannot be nil")
	}
	if *target == nil {
		*target = make(map[string]any)
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode payload json: %w", err)
	}
	return nil
}
