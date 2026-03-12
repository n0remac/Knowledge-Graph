# Discord GraphRAG Bot Spec

## Project Name
**discord-graphrag**

A lightweight Discord chatbot that uses Qwen models plus a small graph-and-vector memory system to remember users, topics, and stable facts from chat.

---

## 1. Overview

This project is a simplified first version of a larger GraphRAG vision.

Instead of ingesting an existing fantasy roleplay corpus, this version starts with **live Discord conversations only**. The bot watches messages, stores them as source records, extracts a small number of structured memories, embeds those memories for semantic retrieval, and uses both graph-style structure and vector search to generate grounded replies.

The point of this version is to validate the core loop:

```text
Discord chat -> structured memory extraction -> graph + vectors -> retrieval -> grounded response
```

This version is intentionally narrow. It is meant to prove that:
- structured memory is useful
- a small ontology is enough to improve continuity
- Qwen can support extraction, retrieval, and response generation
- a graph-shaped memory layer can be built before a full lore or world model

---

## 2. Goals

### Primary goals
- Build a Discord chatbot that responds using Qwen models.
- Persist raw chat messages as source records.
- Extract a small set of structured memory items from chat.
- Store those memory items in a lightweight graph-oriented schema.
- Support semantic retrieval over memories using vector embeddings.
- Generate replies grounded in recent chat and relevant stored memories.
- Make the system inspectable and debuggable.

### What success looks like
- The bot can remember user preferences across sessions.
- The bot can recall ongoing projects, goals, or recurring discussion themes.
- The bot’s replies are more consistent than a recent-window-only chatbot.
- A human can inspect where a remembered fact came from.

---

## 3. Non-Goals

This version does **not** attempt to do the following:
- ingest historical forum or roleplay data
- build a full fantasy world ontology
- extract scenes, timelines, or detailed events
- maintain precise temporal truth models
- do complex entity resolution across large corpora
- require Neo4j or Memgraph
- implement advanced graph reasoning or multi-hop narrative inference
- support multiple retrieval collections or ranking stages at first
- solve long-term canon consistency for an entire game world

These may be added later, but they are explicitly out of scope for v1.

---

## 4. System Summary

The system consists of six main responsibilities:

1. **Discord ingestion**
   - Read user messages from Discord.
   - Ignore bot/system noise.
   - Store raw messages.

2. **Memory extraction**
   - Use Qwen to extract a small JSON structure from chat.
   - Focus on topics, preferences, goals, and simple relationships.

3. **Structured storage**
   - Persist users, messages, topics, facts, and links between them.
   - Treat SQLite as the source of truth.

4. **Vector indexing**
   - Embed facts and summaries.
   - Use vector search to retrieve semantically relevant memories.

5. **Retrieval**
   - Combine recent conversation context, direct memories, and semantic matches.

6. **Response generation**
   - Use Qwen to generate a grounded reply in the configured bot persona.

---

## 5. Core Product Idea

The first useful version is:

> A Discord bot that remembers who people are, what they care about, what they are working on, and what topics come up often.

This is enough to validate the architectural direction before building a richer world-knowledge system.

---

## 6. Architecture

```text
Discord Message
  -> store raw message
  -> run memory extraction
  -> persist facts/topics/links
  -> embed new or updated memories
  -> retrieve recent + relevant memory for next turn
  -> generate reply with Qwen
```

### Recommended storage stack
- **SQLite**: primary structured storage and graph-like memory model
- **Qdrant**: vector search for facts, topics, and summaries
- **Filesystem/cache directory**: logs, prompt traces, extraction artifacts, debug dumps

### Why SQLite first
SQLite is preferred for v1 because:
- it is easy to inspect and debug
- schema changes are lightweight
- graph-like relations can be modeled with normal tables
- it reduces system complexity while validating the architecture

A dedicated graph database can be introduced later if graph depth or traversal complexity increases.

---

## 7. Minimal Ontology

The ontology must stay intentionally small.

### Node types
- **User**: a Discord user
- **Message**: a raw Discord message
- **Topic**: a recurring or meaningful discussion subject
- **Fact**: a normalized memory/assertion derived from one or more messages
- **BotPersona**: optional node representing the bot’s configured identity

### Edge types
- `SENT`: User -> Message
- `MENTIONS_TOPIC`: Message -> Topic
- `ABOUT`: Fact -> User or Topic
- `SAID_BY`: Fact -> User
- `DERIVED_FROM`: Fact -> Message
- `RELATED_TO`: Topic -> Topic
- `INTERACTS_WITH`: optional User -> User

### Why this ontology
This ontology supports:
- source tracking
- per-user memory
- recurring topic awareness
- simple relationship modeling

It avoids the overhead of full general-purpose knowledge representation.

---

## 8. Data Model

### Message
```go
 type Message struct {
     ID        string
     ChannelID string
     AuthorID  string
     Content   string
     Timestamp time.Time
     ReplyToID *string
 }
```

### User
```go
 type User struct {
     ID          string
     Username    string
     DisplayName string
 }
```

### Topic
```go
 type Topic struct {
     ID          string
     Name        string
     Summary     string
     LastSeenAt  time.Time
 }
```

### Fact
```go
 type Fact struct {
     ID           string
     Kind         string // preference | goal | project | relationship | identity | status
     SubjectID    string
     ObjectID     *string
     ValueText    string
     Confidence   float32
     Status       string // candidate | durable
     CreatedAt    time.Time
     LastSeenAt   time.Time
 }
```

### FactSource
```go
 type FactSource struct {
     FactID    string
     MessageID string
 }
```

### TopicLink
```go
 type TopicLink struct {
     FromTopicID string
     ToTopicID   string
     Strength    float32
 }
```

---

## 9. Memory Model

The memory system should not treat every extracted statement as permanent truth.

### Memory classes

#### 1. Ephemeral memory
- Recent chat only
- Not stored as durable knowledge
- Used for immediate conversational continuity

#### 2. Candidate memory
- Newly extracted structured fact
- Possibly relevant but not yet trusted
- May expire or be promoted later

#### 3. Durable memory
- Stable, repeated, or high-confidence fact
- Strong enough to use as long-term memory

### Promotion rules
A fact may be promoted from `candidate` to `durable` if:
- confidence exceeds a chosen threshold, or
- the same fact is extracted multiple times, or
- the user explicitly confirms it, or
- it has high utility in ongoing conversation

### Decay rules
A fact may be deprioritized or archived if:
- it is low-confidence and never repeated
- it becomes stale and irrelevant
- it conflicts with newer confirmed information

This promotion/decay design is more important than a richer ontology in v1.

---

## 10. Extraction Scope

The extractor should be narrow and predictable.

### Extract these categories only
- **preferences**
  - likes dark fantasy
  - prefers stealth gameplay
- **projects/goals**
  - building a campaign map
  - writing guild lore
- **relationships**
  - collaborating with another user
  - running a campaign for a group
- **identity/status**
  - playing as a certain character archetype
  - currently planning a new campaign
- **topics**
  - moon temple
  - necromancy
  - world map

### Do not extract yet
- full event timelines
- detailed causal chains
- world-state canon
- psychological inference
- broad open-ended triples for every sentence

---

## 11. Extraction Contract

Qwen should output a constrained JSON schema.

### Example extraction output
```json
{
  "topics": ["necromancy", "moon temple", "campaign planning"],
  "facts": [
    {
      "kind": "preference",
      "subject_id": "user123",
      "value_text": "likes dark fantasy settings",
      "confidence": 0.87
    },
    {
      "kind": "project",
      "subject_id": "user456",
      "value_text": "is building a world map for the campaign",
      "confidence": 0.78
    }
  ],
  "relationships": [
    {
      "subject_id": "user123",
      "relation": "collaborates_with",
      "object_id": "user456",
      "confidence": 0.72
    }
  ]
}
```

### Extraction prompt requirements
The extractor prompt should instruct the model to:
- only extract stable or likely-relevant information
- prefer explicit information over guesses
- output valid JSON only
- avoid inventing facts not present in the message context
- keep topics short and normalized
- attach confidence scores conservatively

### Extraction source scope
Extraction can run on:
- one message at a time, or
- a short sliding window of recent messages

A short sliding window is often better for relationship or topic extraction.

---

## 12. Storage Design

### SQLite tables
Suggested initial tables:
- `users`
- `messages`
- `topics`
- `facts`
- `fact_sources`
- `message_topics`
- `topic_links`
- `user_relationships`
- `channel_summaries` (optional)

### Source of truth policy
- SQLite is the authoritative source for messages, facts, and links.
- Qdrant is a retrieval acceleration layer, not the canonical datastore.

### Provenance policy
Every fact should be traceable back to one or more message IDs.

At minimum, provenance should include:
- source message ID
- extraction timestamp
- extraction model name
- confidence score

---

## 13. Vector Index Design

### What to embed
Start with only:
- durable facts
- candidate facts above a threshold
- topic summaries
- channel summaries (optional)

### What not to embed initially
- every raw message
- every topic variant
- every relationship edge as its own document

### Suggested collections
A single collection is acceptable for v1 if payloads are tagged with type.

Payload type examples:
- `fact`
- `topic_summary`
- `channel_summary`

You can split collections later if needed.

### Payload fields
Suggested payload fields:
- `kind`
- `subject_id`
- `object_id`
- `topic_ids`
- `channel_id`
- `status`
- `message_ids`
- `updated_at`

---

## 14. Retrieval Strategy

Retrieval should stay simple and deterministic.

### Retrieval inputs
- current message text
- current channel ID
- current speaker
- mentioned users
- recent channel messages

### Retrieval steps

#### 1. Recent context
Always include a recent message window from the current channel.

#### 2. Direct memory fetch
Fetch memories directly connected to:
- current speaker
- explicitly mentioned users
- current channel if channel summaries exist

#### 3. Semantic memory search
Search vector index with the current message text.
Use filters where helpful:
- same channel
- same subject user
- same topic cluster

#### 4. Topic neighborhood
Fetch related topics linked to the current discussion.

#### 5. Assemble evidence bundle
Return a compact bundle containing:
- recent messages
- relevant facts
- relevant topics
- optional summaries
- source references

### Retrieval output shape
```go
 type RetrievalBundle struct {
     RecentMessages []Message
     Facts          []Fact
     Topics         []Topic
     Summaries      []string
     SourceMsgIDs   []string
 }
```

### v1 principle
This is already enough to behave like GraphRAG without needing multi-hop reasoning.

---

## 15. Generation Strategy

### Input to generation
The generator should receive:
- bot persona/configuration
- recent message window
- retrieved facts
- retrieved topics
- optional channel summary
- current user message

### Prompt structure
```text
SYSTEM:
You are {bot_name}.
{persona_description}

CONTEXT:
Recent conversation:
- ...

Relevant memories:
- User123 prefers dark fantasy settings
- User123 is planning a morally gray campaign
- Topic: moon temple mystery

USER MESSAGE:
{message}

Instructions:
- Stay grounded in the provided memory.
- Do not invent private facts.
- If memory is uncertain, phrase carefully.
- Reply naturally for Discord.
```

### Style requirements
- natural and concise
- context-aware
- does not overstate uncertain memory
- can acknowledge uncertainty
- remains consistent with persona

### Optional citations
For debugging or admin modes, the system may include message IDs or internal citations.
For normal conversation, citations should usually stay hidden.

---

## 16. Bot Behavior

### Core bot responsibilities
- subscribe to target channels
- store messages
- decide whether to reply
- optionally extract memory from all observed messages
- retrieve relevant memory when replying
- generate persona-consistent responses

### Reply policy
The bot should not necessarily reply to every message.
Possible triggers:
- direct mention
- reply to bot
- configured channel mode
- explicit command

### Admin/debug commands
Strongly recommended for early development:
- `!memory @user`
- `!topics`
- `!fact <id>`
- `!forget <id>`
- `!summary`
- `!reextract <message_id>`

These are critical for debugging extraction quality and memory usefulness.

---

## 17. Proposed Package Layout

```text
discord-graphrag/
├── cmd/
│   └── bot/
├── internal/
│   ├── discordbot/
│   ├── store/
│   ├── extract/
│   ├── retrieve/
│   ├── generate/
│   ├── embed/
│   ├── memory/
│   └── models/
├── configs/
│   ├── prompts/
│   └── bot.yaml
├── data/
│   ├── sqlite/
│   ├── cache/
│   └── debug/
└── README.md
```

### Package responsibilities

#### `discordbot/`
Discord runtime, event handling, reply triggers, command handling.

#### `store/`
SQLite schema, queries, migrations, persistence logic.

#### `extract/`
Prompt building, Qwen JSON extraction, validation, normalization.

#### `retrieve/`
Recent-message fetch, direct memory fetch, semantic search, evidence assembly.

#### `generate/`
Bot response prompt construction and model invocation.

#### `embed/`
Embedding generation and vector upsert/search.

#### `memory/`
Promotion, decay, dedupe, summaries, memory policy.

#### `models/`
Shared types and interfaces.

---

## 18. Suggested Interfaces

### Extractor
```go
 type Extractor interface {
     Extract(ctx context.Context, window []Message) (ExtractionResult, error)
 }
```

### Embedder
```go
 type Embedder interface {
     Embed(ctx context.Context, texts []string) ([][]float32, error)
 }
```

### Retriever
```go
 type Retriever interface {
     Retrieve(ctx context.Context, input RetrievalInput) (RetrievalBundle, error)
 }
```

### Generator
```go
 type Generator interface {
     Generate(ctx context.Context, input GenerationInput) (string, error)
 }
```

### Store
```go
 type Store interface {
     SaveMessage(ctx context.Context, msg Message) error
     SaveExtraction(ctx context.Context, result ExtractionResult) error
     GetRecentMessages(ctx context.Context, channelID string, limit int) ([]Message, error)
     GetFactsForUser(ctx context.Context, userID string) ([]Fact, error)
     SearchTopics(ctx context.Context, query string) ([]Topic, error)
 }
```

---

## 19. Dedupe and Normalization Rules

### Fact dedupe
Facts should be normalized before insert or merge using:
- normalized subject ID
- normalized kind
- canonicalized value text

Examples:
- “likes dark fantasy”
- “prefers dark fantasy settings”

These may map to the same memory item if normalization says they are equivalent enough.

### Topic normalization
Topics should be normalized for:
- case
- plural/singular variants where sensible
- punctuation
- obvious aliases

Example:
- `moon temple`
- `the moon temple`

These may map to one topic.

---

## 20. Summaries

Summaries are optional in the earliest version, but should be planned for.

### Useful summary types
- **channel summary**: rolling summary of recent discussion in a channel
- **topic summary**: short summary of what is usually meant by a topic
- **user summary**: compact overview of a user’s durable facts

### Why summaries matter
They reduce retrieval noise and improve prompt efficiency.

---

## 21. Milestones

### Milestone 1: basic Discord bot
Deliverables:
- Discord bot connects and listens
- messages are stored in SQLite
- Qwen chat reply works
- no memory extraction yet

Success criteria:
- stable message ingestion and reply loop

### Milestone 2: structured extraction
Deliverables:
- topic and fact extraction from messages or windows
- extraction results stored in SQLite
- source message linkage preserved
- admin inspection commands added

Success criteria:
- bot can remember basic user preferences and projects

### Milestone 3: vector memory retrieval
Deliverables:
- embeddings for facts and topic summaries
- semantic search over memories
- retrieval bundle injected into generation prompt

Success criteria:
- bot recalls semantically related older discussion

### Milestone 4: memory lifecycle
Deliverables:
- candidate vs durable fact status
- promotion and decay rules
- dedupe improvements
- basic summaries

Success criteria:
- memory quality improves over time instead of degrading

### Milestone 5: graph-aware refinement
Deliverables:
- direct user/topic neighborhood fetch
- related-topic expansion
- optional lightweight relationship retrieval

Success criteria:
- bot shows clearer continuity across users and recurring subjects

---

## 22. Evaluation

### Functional checks
- Does the bot remember explicit preferences?
- Does the bot remember ongoing projects?
- Can a developer inspect where a fact came from?
- Does the bot avoid overclaiming uncertain memory?

### Quality checks
- precision of extracted facts
- usefulness of retrieved memories
- response coherence
- hallucination rate
- memory bloat/noise over time

### Example evaluation prompts
- “Do you remember what kind of campaign I wanted?”
- “What were we saying about the moon temple?”
- “Who was helping me with the map?”
- “What do you know about my character preferences?”

### Early target metrics
- extracted facts are mostly useful rather than noisy
- provenance is available for nearly all durable facts
- replies feel more informed than recent-window-only chat

---

## 23. Risks and Mitigations

### Risk: extraction noise
Qwen may over-extract or invent weak facts.

Mitigations:
- narrow JSON schema
- conservative prompt wording
- candidate vs durable memory separation
- explicit provenance tracking

### Risk: memory bloat
Too many low-value memories reduce retrieval quality.

Mitigations:
- promotion thresholds
- pruning/decay
- dedupe
- summaries

### Risk: overconfident replies
The bot may speak as if uncertain memories are certain.

Mitigations:
- pass confidence into generation context
- instruct generator to hedge uncertain memory
- prefer durable facts in prompts

### Risk: overcomplicated infrastructure too early
Adding a graph database or complex retrieval too soon may slow progress.

Mitigations:
- SQLite first
- single vector collection first
- defer advanced graph layers until usefulness is proven

---

## 24. Future Expansion Path

If v1 works, future versions can add:
- ingestion of historical roleplay/forum data
- richer entity types such as character, location, faction, item
- event extraction and timelines
- scene summaries
- dedicated graph database
- claim validation workflows
- multi-agent extraction/review pipelines
- richer world-state memory for fantasy roleplay systems

This first version should be treated as the proving ground for those later steps.

---

## 25. Implementation Priorities

### Highest priority
1. raw message storage
2. extraction JSON contract
3. provenance-preserving fact storage
4. simple retrieval bundle
5. grounded generation
6. debug commands

### Medium priority
1. embeddings
2. topic links
3. summaries
4. candidate/durable promotion

### Lower priority
1. advanced relationship modeling
2. graph database migration
3. multi-hop graph retrieval
4. verification agent layers

---

## 26. Final Design Principle

This project should treat the graph not as a perfect world model, but as **structured conversational memory**.

That means:
- facts are linked to source messages
- confidence matters
- memory can be revised
- not everything said becomes permanent truth
- usefulness is more important than ontology depth in v1

If this version succeeds, it will provide a strong, testable foundation for a later fantasy-dataset GraphRAG system.
