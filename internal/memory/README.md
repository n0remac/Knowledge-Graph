# Memory

`internal/memory` is the project's passive memory ingestion layer. It persists raw messages, stores claim-extraction results, tracks vector-document metadata, and exposes summary snapshots for admin views.

It does not own Discord event handling, embedding-vector storage, or semantic search. Those sit around it in [`internal/discordbot/runtime.go`](../discordbot/runtime.go#L70-L103), [`internal/embedding/index.go`](../embedding/index.go#L81-L126), and [`internal/research/memory_source.go`](../research/memory_source.go#L46-L180).

## Code Map

- [`store.go`](./store.go#L23-L621): SQLite-backed persistence, schema setup, mutating writes, lookup methods, sanitization helpers, and notifier/telemetry hooks.
- [`collector.go`](./collector.go#L15-L214): the ingest pipeline that turns one raw message into stored claims plus vector documents.
- [`snapshot.go`](./snapshot.go#L9-L172): read-model helpers used by the admin UI.

## Main Types

- [`Store`](./store.go#L23-L28): durable SQLite store for messages, extractions, claims, and vector-document metadata.
- [`ClaimExtractionRecord`](./store.go#L30-L38): the stored result of running claim extraction on a message.
- [`ClaimRecord`](./store.go#L40-L50): a normalized, persisted claim row.
- [`VectorDocumentRecord`](./store.go#L52-L65): metadata for a document that should exist in the embedding index. This table stores indexing state and payload JSON, not the embedding vector itself.
- [`Collector`](./collector.go#L27-L43): orchestration layer that persists a message, runs extraction, prepares vector documents, and asks the embedding index to upsert them.
- [`Snapshot`](./snapshot.go#L15-L23): aggregate counts plus recent rows for the admin dashboard.

## What Lives In SQLite

`Store.init` creates four tables in SQLite: [`messages`, `message_extractions`, `claims`, and `vector_documents`](./store.go#L414-L468).

- `messages` stores the normalized raw message, including `reply_to_message_id`.
- `message_extractions` stores the raw extraction payload and extraction status for one message.
- `claims` stores normalized claim rows derived from the extraction result.
- `vector_documents` stores document metadata, payload JSON, collection name, indexing timestamps, and last error text.

The actual vectors are not stored here. `Collector` passes `embedding.Document` values to [`embedding.Index.UpsertDocuments`](../embedding/index.go#L81-L126), which embeds them and writes the vectors to Qdrant.

## Overall Workflow

1. The bot runtime creates the store and collector in [`discordbot.NewRuntime`](../discordbot/runtime.go#L70-L103).
2. When a message arrives on an observed channel, the runtime calls [`Collector.IngestMessage`](./collector.go#L45-L119) from [`onMessageCreate`](../discordbot/runtime.go#L239-L247).
3. `IngestMessage` sanitizes the message and applies collector-level policy: blank messages and assistant-authored messages are ignored entirely in [`collector.go`](./collector.go#L50-L56).
4. The raw message is persisted through [`Store.SaveMessage`](./store.go#L112-L145), which upserts by `message_id`.
5. The collector runs the configured `ClaimsExtractor` with the current message plus optional reply target in [`collector.go`](./collector.go#L63-L77). The runtime wires in the default extractor via [`claimextract.New(...)`](../discordbot/runtime.go#L97-L100).
6. The extraction result is stored through [`Store.SaveClaimsExtraction`](./store.go#L147-L234). That method upserts the extraction row, deletes any old claims for the message, and inserts the new normalized claims inside one transaction.
7. Claim IDs are deterministic. [`claimID`](./store.go#L572-L582) hashes `message_id` plus a normalized `(subject, predicate, object)` triple, which makes repeated extraction of the same claim update the same row instead of creating a new identity.
8. The collector turns the saved message and claims into vector docs with [`buildVectorDocuments`](./collector.go#L122-L181):
   - one `message:<message_id>` document for the raw message text
   - one `claim:<claim_id>` document per claim, with text rendered by [`renderClaimText`](./collector.go#L184-L186)
9. Those vector-document records are first persisted locally with [`Store.UpsertVectorDocuments`](./store.go#L236-L287), usually in the initial `pending` state.
10. The collector then calls [`embedding.Index.UpsertDocuments`](../embedding/index.go#L81-L126), which embeds the documents, ensures the Qdrant collection exists, and upserts the points.
11. If indexing fails, the collector marks those local records as failed with [`MarkVectorDocumentsFailed`](./store.go#L293-L299) and returns the indexing error from [`collector.go`](./collector.go#L91-L101). If indexing succeeds, it marks them indexed with [`MarkVectorDocumentsIndexed`](./store.go#L289-L291) and stores the collection name and timestamp in [`collector.go`](./collector.go#L103-L105).
12. Downstream, the research subsystem searches those indexed documents in [`research.MemorySource.Search`](../research/memory_source.go#L46-L93). Claim hits are re-hydrated from [`Store.GetMessageByID`](./store.go#L301-L316) so the final artifact uses authoritative parent-message metadata rather than only the vector payload in [`research/memory_source.go`](../research/memory_source.go#L139-L180).

## Store Behavior

### SQLite configuration

[`NewStore`](./store.go#L67-L96) opens a single-connection SQLite database, then applies [`PRAGMA busy_timeout`, `journal_mode = WAL`, and `foreign_keys = ON`](./store.go#L400-L412). The busy timeout matters in practice: [`TestStoreSaveMessageWaitsForTransientWriteLock`](./store_test.go#L123-L205) verifies that a write waits for a transient lock instead of failing immediately.

### Upsert and replacement semantics

- [`SaveMessage`](./store.go#L112-L145) updates existing rows when the same `message_id` is written again.
- [`SaveClaimsExtraction`](./store.go#L147-L234) replaces the full claim set for a message by deleting existing claims and reinserting the current extraction result.
- [`UpsertVectorDocuments`](./store.go#L236-L287) updates existing vector-document rows by `document_id`.

Those semantics make repeated ingestion effectively idempotent for the same message. [`TestCollectorIngestsMessageClaimsAndVectorDocuments`](./collector_test.go#L14-L91) confirms that re-ingesting the same message does not multiply vector documents.

### Status normalization

- Extraction status is normalized by [`normalizeStatus`](./store.go#L548-L559) to `ok`, `partial`, or `failed`.
- Vector-document status is normalized by [`normalizeIndexStatus`](./store.go#L561-L570) to `pending`, `indexed`, or `failed`.

### Notifications and telemetry

Every mutating path calls [`publishChange`](./store.go#L616-L621), and [`SetNotifier`](./store.go#L105-L110) lets the runtime attach the shared admin notifier. The admin dashboard consumes that through [`renderMemorySection`](../../web/admin.go#L88-L130) and refreshes the memory vertical on [`adminstream.VerticalMemory`](../../web/admin.go#L422-L428). The store and collector also emit telemetry through [`Store.emit`](./store.go#L599-L614) and [`Collector.emit`](./collector.go#L199-L214).

## Snapshot API

[`Store.Snapshot`](./snapshot.go#L34-L72) is the read-side summary API for the admin UI. It returns:

- total counts for messages, extractions, claims, and vector documents
- recent messages ordered by `timestamp_unix_ms` in [`recentMessages`](./snapshot.go#L82-L103)
- recent claims ordered by extraction time in [`recentClaims`](./snapshot.go#L105-L126)
- recent vector documents ordered by `indexed_at_unix_ms` in [`recentVectorDocuments`](./snapshot.go#L128-L156)

The admin page uses that snapshot directly in [`web/admin.go`](../../web/admin.go#L100-L127).

## Important Package Boundaries

- The `Store` is lower-level than the `Collector`. For example, [`SaveMessage`](./store.go#L112-L145) will accept a blank `Content` string, but [`Collector.IngestMessage`](./collector.go#L54-L56) explicitly skips blank and assistant messages. That policy lives in the collector, not the store.
- `vector_documents` is a tracking table, not the vector index. The actual vector write happens in [`embedding.Index.UpsertDocuments`](../embedding/index.go#L81-L126).
- Semantic retrieval is downstream. The memory package prepares the searchable documents; [`research.MemorySource.Search`](../research/memory_source.go#L46-L93) consumes them later.
- `Collector.IngestMessage` persists as much state as it can before returning. Because it returns [`result.Err`](./collector.go#L116-L118) only after message persistence, extraction persistence, vector-doc creation, and successful indexing, callers can observe stored memory even when extraction ultimately reports an error.

## Tests Worth Reading

- [`store_test.go`](./store_test.go#L13-L121): round-trips messages, extractions, claims, and vector-document metadata across a store reopen.
- [`store_test.go`](./store_test.go#L123-L205): verifies the SQLite busy-timeout behavior under a transient write lock.
- [`collector_test.go`](./collector_test.go#L14-L91): covers the happy-path ingest flow and duplicate-ingest behavior.
- [`collector_test.go`](./collector_test.go#L93-L127): confirms the collector ignores blank and assistant messages.
- [`collector_test.go`](./collector_test.go#L129-L183): confirms indexing failures still leave persisted message/claim state and mark vector documents as failed.
