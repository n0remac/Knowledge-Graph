package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/claimextract"
	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

type ClaimsExtractor interface {
	Extract(context.Context, claimextract.Input) claimextract.Result
}

type DocumentIndexer interface {
	UpsertDocuments(context.Context, string, []embedding.Document) (string, error)
}

type CollectOptions struct {
	ReplyTarget *models.RawMessage
}

type Collector struct {
	store          *Store
	extractor      ClaimsExtractor
	index          DocumentIndexer
	embeddingModel string
	telemetry      *telemetry.Manager
}

func NewCollector(store *Store, extractor ClaimsExtractor, index DocumentIndexer, embeddingModel string, manager *telemetry.Manager) *Collector {
	return &Collector{
		store:          store,
		extractor:      extractor,
		index:          index,
		embeddingModel: strings.TrimSpace(embeddingModel),
		telemetry:      manager,
	}
}

func (c *Collector) IngestMessage(ctx context.Context, input models.RawMessage, options CollectOptions) error {
	if c == nil || c.store == nil || c.extractor == nil || c.index == nil || c.embeddingModel == "" {
		return fmt.Errorf("memory collector is not initialized")
	}

	message := sanitizeRawMessage(input)
	if message.MessageID == "" || message.ConversationID == "" || message.AuthorID == "" || message.AuthorRole == "" {
		return fmt.Errorf("memory collector received invalid message")
	}
	if strings.TrimSpace(message.Content) == "" || message.AuthorRole == "assistant" {
		return nil
	}

	savedMessage, err := c.store.SaveMessage(ctx, message)
	if err != nil {
		return err
	}

	extractedAtUnixMs := time.Now().UTC().UnixMilli()
	result := c.extractor.Extract(ctx, claimextract.Input{
		CurrentMessage: savedMessage,
		ReplyTarget:    options.ReplyTarget,
	})
	extraction := ClaimExtractionRecord{
		MessageID:         savedMessage.MessageID,
		ConversationID:    savedMessage.ConversationID,
		Claims:            result.Claims,
		Status:            result.Status,
		Model:             result.Model,
		Raw:               result.Raw,
		ExtractedAtUnixMs: extractedAtUnixMs,
	}
	claims, err := c.store.SaveClaimsExtraction(ctx, extraction)
	if err != nil {
		return err
	}

	documents, records := buildVectorDocuments(savedMessage, claims, c.embeddingModel)
	documentIDs := make([]string, 0, len(records))
	for _, record := range records {
		documentIDs = append(documentIDs, record.DocumentID)
	}
	if err := c.store.UpsertVectorDocuments(ctx, records); err != nil {
		return err
	}

	collectionName, indexErr := c.index.UpsertDocuments(ctx, c.embeddingModel, documents)
	if indexErr != nil {
		if err := c.store.MarkVectorDocumentsFailed(ctx, documentIDs, indexErr); err != nil {
			return err
		}
		c.emitError(ctx, "memory_index_error", "failed to index vector documents", indexErr, map[string]any{
			"message_id":      savedMessage.MessageID,
			"conversation_id": savedMessage.ConversationID,
			"document_count":  len(documents),
		})
		return indexErr
	}
	if err := c.store.MarkVectorDocumentsIndexed(ctx, documentIDs, collectionName, time.Now().UTC().UnixMilli()); err != nil {
		return err
	}

	c.emit(ctx, "memory_ingested", "memory collector ingested message", map[string]any{
		"message_id":        savedMessage.MessageID,
		"conversation_id":   savedMessage.ConversationID,
		"claim_count":       len(claims),
		"document_count":    len(documents),
		"embedding_model":   c.embeddingModel,
		"collection_name":   collectionName,
		"extraction_status": extraction.Status,
	})
	if result.Err != nil {
		return result.Err
	}
	return nil
}

func buildVectorDocuments(message models.RawMessage, claims []ClaimRecord, embeddingModel string) ([]embedding.Document, []VectorDocumentRecord) {
	embeddingModel = strings.TrimSpace(embeddingModel)

	messagePayload := map[string]any{
		"kind":              "message",
		"message_id":        message.MessageID,
		"conversation_id":   message.ConversationID,
		"author_id":         message.AuthorID,
		"author_role":       message.AuthorRole,
		"timestamp_unix_ms": message.TimestampUnixMs,
	}
	messageDocID := "message:" + message.MessageID
	documents := []embedding.Document{{
		DocumentID: messageDocID,
		Text:       message.Content,
		Payload:    clonePayload(messagePayload),
	}}
	records := []VectorDocumentRecord{{
		DocumentID:     messageDocID,
		Kind:           "message",
		MessageID:      message.MessageID,
		ConversationID: message.ConversationID,
		Content:        message.Content,
		EmbeddingModel: embeddingModel,
		IndexStatus:    "pending",
		Payload:        clonePayload(messagePayload),
	}}

	for _, claim := range claims {
		text := renderClaimText(claim)
		payload := map[string]any{
			"kind":              "claim",
			"claim_id":          claim.ClaimID,
			"message_id":        claim.MessageID,
			"conversation_id":   claim.ConversationID,
			"source_message_id": claim.SourceMessageID,
			"subject":           claim.Subject,
			"predicate":         claim.Predicate,
			"object":            claim.Object,
		}
		docID := "claim:" + claim.ClaimID
		documents = append(documents, embedding.Document{
			DocumentID: docID,
			Text:       text,
			Payload:    clonePayload(payload),
		})
		records = append(records, VectorDocumentRecord{
			DocumentID:     docID,
			Kind:           "claim",
			MessageID:      claim.MessageID,
			ClaimID:        claim.ClaimID,
			ConversationID: claim.ConversationID,
			Content:        text,
			EmbeddingModel: embeddingModel,
			IndexStatus:    "pending",
			Payload:        clonePayload(payload),
		})
	}

	return documents, records
}

func renderClaimText(claim ClaimRecord) string {
	return strings.TrimSpace(claim.Subject) + " | " + strings.TrimSpace(claim.Predicate) + " | " + strings.TrimSpace(claim.Object)
}

func clonePayload(input map[string]any) map[string]any {
	if len(input) == 0 {
		return make(map[string]any)
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (c *Collector) emit(ctx context.Context, kind, summary string, payload map[string]any) {
	if c == nil || c.telemetry == nil {
		return
	}
	c.telemetry.Emit(ctx, telemetry.StageMemory, kind, summary, payload)
}

func (c *Collector) emitError(ctx context.Context, kind, summary string, err error, payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any, 1)
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	c.emit(ctx, kind, summary, payload)
}
