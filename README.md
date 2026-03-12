# Knowledge-Graph

Discord bot with a structured conversational knowledge graph (no embeddings yet).

## Current behavior

For each non-bot message the bot can read:
1. Persist the raw message.
2. Extract structured topics/facts with Qwen via Ollama.
3. Upsert topics, facts, and provenance links in the graph store.
4. Retrieve recent messages + relevant facts/topics for the current user.
5. Generate a grounded reply using the retrieved memory context.

## Setup

### Required

- `DISCORD_BOT_TOKEN`

### Optional

- `OLLAMA_BASE_URL` (default: `http://localhost:11434`)
- `OLLAMA_CHAT_MODEL` (default: `qwen2.5:1.5b-instruct`)
- `OLLAMA_EXTRACT_MODEL` (default: value of `OLLAMA_CHAT_MODEL`)
- `BOT_PERSONA` (default: `You are a helpful Discord assistant.`)
- `GRAPH_STORE_PATH` (default: `data/graph-store.json`)
- `SQLITE_PATH` (legacy fallback env var)
- `RECENT_MESSAGE_LIMIT` (default: `12`)
- `RECALL_FACT_LIMIT` (default: `12`)
- `RECALL_TOPIC_LIMIT` (default: `8`)
- `REQUEST_TIMEOUT_SECONDS` (default: `45`)

### Discord app settings

- Enable `Message Content Intent`.
- Invite the bot with:
  - `View Channels`
  - `Read Message History`
  - `Send Messages`

## Run

```bash
export DISCORD_BOT_TOKEN=your_bot_token
export OLLAMA_BASE_URL=http://localhost:11434
export OLLAMA_CHAT_MODEL=qwen2.5:1.5b-instruct
go run .
```

## Data model (v1, no vectors)

Graph entities and links:
- users
- messages
- topics
- message->topic links
- facts
- fact->message provenance links

Facts are promoted from `candidate` to `durable` when confidence is high or the same fact is observed repeatedly.
