# Research

`internal/research` contains the reusable core of the project's research flow. It owns the shared request and artifact types, request normalization, source planning, source registration, memory-backed search, and context rendering.

It does not own HTTP transport, run persistence, or vector ingestion. Those live around it in [`web/research_data.go`](../../web/research_data.go#L41-L90), [`internal/researchtest/service.go`](../researchtest/service.go#L40-L110), and [`internal/researchtest/service.go`](../researchtest/service.go#L127-L213).

## Code Map

- [`types.go`](./types.go#L5-L84): shared constants, source interfaces, request/plan types, normalized artifact shape, and rendered context shape.
- [`planner.go`](./planner.go#L8-L60): request validation plus the current rule-based planner.
- [`registry.go`](./registry.go#L9-L86): source registration and capability lookup.
- [`memory_source.go`](./memory_source.go#L20-L243): the current searchable source, backed by the memory store and embedding index.
- [`context.go`](./context.go#L8-L84): selects a small artifact subset and builds the human-readable evidence brief.

## Main Types

- [`SearchRequest`](./types.go#L31-L35): input to planning and source execution. It carries a free-text query, an optional `ConversationID`, and `TopK`.
- [`Plan`](./types.go#L37-L46): planner output. Today it is a list of source-specific queries, even though the current planner emits only one.
- [`ResearchArtifact`](./types.go#L56-L70): the canonical search hit shape returned by sources. It includes source identity, content, conversation metadata, and retrieval provenance.
- [`ResearchContext`](./types.go#L80-L84): a compact subset of artifacts plus pre-rendered evidence text for downstream consumers.

The interfaces in [`types.go`](./types.go#L16-L29) are intentionally broader than the current implementation. `SearchableSource` is used today; `FetchableSource` exists as a future extension point but is not used in the current runtime.

## Overall Workflow

1. Searchable memory is prepared upstream by [`memory.Collector.IngestMessage`](../memory/collector.go#L45-L119), which stores the raw message, extracts claims, and upserts message and claim vector documents built by [`buildVectorDocuments`](../memory/collector.go#L122-L181).
2. The live research path enters through [`ResearchRunHandler`](../../web/research_data.go#L41-L65), which calls [`Service.RunQuery`](../researchtest/service.go#L127-L213).
3. `RunQuery` normalizes the incoming request through [`NormalizeSearchRequest`](./planner.go#L20-L35), allocates a run record, and asks [`RulePlanner.Plan`](./planner.go#L38-L56) for a source plan.
4. The current planner is intentionally simple: [`NewRulePlanner`](./planner.go#L12-L18) defaults to the `memory` source, and [`Plan`](./planner.go#L38-L56) emits one normalized [`PlannedSourceQuery`](./types.go#L37-L42).
5. For each planned query, the service resolves the source through [`SourceRegistry.Searchable`](./registry.go#L43-L50) and calls `Search`.
6. The current source implementation is [`MemorySource.Search`](./memory_source.go#L46-L93). It validates the request again, adds a `conversation_id` filter only when a scope is provided, and calls [`embedding.Index.Search`](../embedding/index.go#L129-L170).
7. Each vector hit is converted into a [`ResearchArtifact`](./types.go#L56-L70) by [`resultToArtifact`](./memory_source.go#L95-L105):
   - Message hits go through [`messageArtifact`](./memory_source.go#L107-L137), which maps payload fields directly into a normalized artifact.
   - Claim hits go through [`claimArtifact`](./memory_source.go#L139-L180), which hydrates author, conversation, and timestamp from the parent message via [`memory.Store.GetMessageByID`](../memory/store.go#L301-L315) instead of trusting the vector payload alone.
8. Duplicate hits are collapsed by `ArtifactID` inside [`MemorySource.Search`](./memory_source.go#L67-L92). The winner is chosen by [`artifactBetter`](./memory_source.go#L201-L209): higher score first, then lower rank, then stable `ArtifactID` ordering.
9. The service passes the ordered artifacts to [`BuildResearchContext`](./context.go#L8-L44), which selects up to [`MaxContextArtifacts`](./types.go#L5-L14), builds snippets with [`buildEvidenceSnippet`](./context.go#L60-L66), and renders a fixed three-section brief: query, conversation scope, and evidence.
10. After context creation, the service copies the `"selected for context preview"` reason back onto the full artifact list with [`applySelectionReasons`](../researchtest/service.go#L294-L313) and persists the run output to `result.json` and `artifacts.json` in [`RunQuery`](../researchtest/service.go#L193-L212).

## What The Memory Source Actually Searches

The research package does not query SQLite tables directly for semantic retrieval. It searches the embedding index populated by the memory subsystem:

- [`memory.Collector.IngestMessage`](../memory/collector.go#L45-L119) creates vector documents for both messages and extracted claims.
- [`embedding.Index.Search`](../embedding/index.go#L129-L170) embeds the query with Ollama, searches the configured Qdrant collection, and returns ranked hits with payload metadata.
- [`MemorySource.Search`](./memory_source.go#L46-L93) turns those hits into stable, package-level artifacts.

This means research results are only as good as the upstream ingestion/indexing pipeline. If a message was never ingested, or a claim was never indexed, this package has nothing to retrieve.

## Current Behaviors And Constraints

- Request normalization is enforced at multiple boundaries. Both [`RulePlanner.Plan`](./planner.go#L38-L56) and [`MemorySource.Search`](./memory_source.go#L46-L54) depend on [`NormalizeSearchRequest`](./planner.go#L20-L35), so empty queries and invalid `top_k` values fail early.
- The planner is source-agnostic in shape but not in behavior. Today it only emits one `memory` query, so there is no multi-source fan-out or cross-source reranking yet.
- Context selection is order-preserving, not score-aware on its own. [`selectContextArtifacts`](./context.go#L46-L58) keeps the first `MaxContextArtifacts` items it receives, so callers are responsible for sending artifacts in the order they want previewed.
- `ResearchArtifact` already has fields like [`ExternalID`, `URL`, and `Metadata`](./types.go#L56-L70) that make sense for future external sources, even though the current `MemorySource` only uses `Metadata`.
- `SourceRegistry` supports both searchable and fetchable capabilities through [`Searchables`](./registry.go#L52-L68) and [`Fetchables`](./registry.go#L70-L86), but the current orchestration path only resolves searchable sources.

## Tests Worth Reading

- [`planner_test.go`](./planner_test.go#L5-L32): confirms whitespace normalization, default `TopK`, and planner output.
- [`registry_test.go`](./registry_test.go#L12-L37): confirms source registration and searchable lookup.
- [`memory_source_test.go`](./memory_source_test.go#L13-L196): covers message normalization, claim hydration, conversation filtering, and deduplication-by-best-score.
- [`context_test.go`](./context_test.go#L8-L31): confirms artifact limit and the fixed section order of the rendered brief.
- [`internal/researchtest/service_test.go`](../researchtest/service_test.go#L25-L87): exercises the end-to-end flow with fake Ollama and Qdrant services.
